package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TODO add sigint cache write to disk
// TODO add verification for the returned cached path, is the recovered cache still valid?
// TODO map keys will be hashes of filepaths, mod times recovered on startup, cache access done by hashing
type CacheEntry struct {
	path    string
	modTime time.Time
}

type BinaryHandlerCache interface {
	getVal(path string) (CacheEntry, bool)
	setVal(key string, val CacheEntry)
}

type BinaryHandlerCacheHashMap struct {
	cache map[string]CacheEntry
}

func (binaryHandlerCache BinaryHandlerCacheHashMap) getVal(path string) (CacheEntry, bool) {
	u, ok := binaryHandlerCache.cache[path]

	return u, ok
}

func (binaryHandlerCache *BinaryHandlerCacheHashMap) setVal(key string, val CacheEntry) {
	binaryHandlerCache.cache[key] = val
}

type BinaryHandler interface {
	handleBinary(path string) (string, error)
	checkIsBinary(path string) (bool, error)

	// return true on change
	addBinaryType(binaryType SpecificHandlerEntry, overwite bool) bool
}

type SpecificHandlerEntry struct {
	magic   []byte
	handler func(string) (string, error)
}

type HeuristicBinaryHandler struct {
	cache            BinaryHandlerCache
	specificHandlers []SpecificHandlerEntry
}

// requires pdftotext
// TODO return temp path from here, rename will be done by the cache handling
func pdfHandler(path string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	base = filepath.Join(base, "lf")

	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}

	wpathTmp, err := os.CreateTemp(base, "pdftotext-*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file:\n%w", err)
	}
	defer os.Remove(wpathTmp.Name())

	wpath := filepath.Join(base, rand.Text()+".txt")

	executable := "pdftotext"
	args := []string{"-nopgbrk", "-layout", "-enc", "UTF-8", "-f", "1", "-l", "20", path, wpathTmp.Name()}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, executable, args...)
	if err = cmd.Run(); err != nil {
		wpathTmp.Close()
		return "", fmt.Errorf("pdftotext returned non-zero exit code:\n %w", err)
	}

	wpathTmp.Close()

	if err := os.Rename(wpathTmp.Name(), wpath); err != nil {
		return "", fmt.Errorf("cache temporary couldnt be renamed:\n %w", err)
	}
	return wpath, nil
}

func RegisterHandlers(handler BinaryHandler) error {
	// TODO check if pdftotext is installed before adding the handler
	magicPdf := []byte{0x25, 0x50, 0x44, 0x46, 0x2d}
	pdfEntry := SpecificHandlerEntry{magic: magicPdf, handler: pdfHandler}
	if !handler.addBinaryType(pdfEntry, true) {
		return fmt.Errorf("pdf handler couldnt be added to the specific handler map")
	}

	return nil
}

// heristic handler
func (binaryHandler *HeuristicBinaryHandler) handleBinary(path string) (string, error) {
	cacheRet, ok := binaryHandler.cache.getVal(path)
	if ok {
		if _, err := os.Stat(cacheRet.path); err == nil {

			info, err := os.Stat(path)
			if err == nil && info.ModTime().Before(cacheRet.modTime) {
				return cacheRet.path, nil
			}
			os.Remove(cacheRet.path)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("error when opening path:\n %w", err)
	}
	defer f.Close()

	buf := make([]byte, 32)
	if _, err = f.Read(buf); err != nil && err != io.EOF {
		return "", fmt.Errorf("error when reading file:\n %w", err)
	}

	done := make(chan int, len(binaryHandler.specificHandlers))
	idx := -1

	if len(binaryHandler.specificHandlers) > 0 {
		for i := range len(binaryHandler.specificHandlers) {
			go func() {
				if bytes.HasPrefix(buf, binaryHandler.specificHandlers[i].magic) {
					done <- i
				} else {
					done <- -1
				}
			}()
		}

		for range len(binaryHandler.specificHandlers) {
			if idx = <-done; idx != -1 {
				break
			}
		}
	}

	if idx == -1 {
		return "", fmt.Errorf("no handlers found for the file")
	}

	// TODO rename the returned temporary path to sha256(text) + ".extension"
	ret, err := binaryHandler.specificHandlers[idx].handler(path)
	if err != nil {
		return "", fmt.Errorf("error occured in the specific handler:\n %w", err)
	}

	if _, err = os.Stat(ret); err != nil {
		return "", fmt.Errorf("the specific handler returned a bad path:\n%w", err)
	}

	binaryHandler.cache.setVal(path, CacheEntry{path: strings.Clone(ret), modTime: time.Now()})
	return ret, nil
}

func (binaryHandler *HeuristicBinaryHandler) addBinaryType(entry SpecificHandlerEntry, overwrite bool) bool {
	done := make(chan int, len(binaryHandler.specificHandlers))
	idx := -1

	if len(binaryHandler.specificHandlers) > 0 {
		for i := range len(binaryHandler.specificHandlers) {
			go func() {
				if bytes.Equal(binaryHandler.specificHandlers[i].magic, entry.magic) {
					done <- i
				} else {
					done <- -1
				}
			}()
		}

		for range len(binaryHandler.specificHandlers) {
			if idx = <-done; idx != -1 {
				break
			}
		}
	}

	if idx == -1 {
		binaryHandler.specificHandlers = append(binaryHandler.specificHandlers, entry)
		return true
	}
	if overwrite {
		binaryHandler.specificHandlers[idx] = entry
		return true
	}
	return false
}

// TODO switch with isHandleable and check for magic words instead. some pdfs dont have 0x00 in first few bytes
func (binaryHandler HeuristicBinaryHandler) checkIsBinary(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("error when opening path:\n %w", err)
	}

	defer f.Close()

	buf := make([]byte, 8000)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("error when reading file:\n %w", err)
	}
	return bytes.IndexByte(buf[:n], 0x00) != -1, nil
}
