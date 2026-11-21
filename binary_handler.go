package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

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

type BinaryType interface {
	isThisBinary(path string) (bool, error)
	binaryHandler(path string) (string, error)
}

type BinaryHandler interface {
	handleBinary(path string) (string, error)
	checkIsBinary(path string) (bool, error)

	// return true on change
	addBinaryType(binaryType BinaryType, overwite bool) bool
}

type SpecificHandlerEntry struct {
	magic   []byte
	handler func(string) (string, error)
}

type HeuristicBinaryHandler struct {
	cache            BinaryHandlerCache
	specificHandlers []SpecificHandlerEntry
}

// heristic handler
func (binaryHandler *HeuristicBinaryHandler) handleBinary(path string) (string, error) {
	cacheRet, ok := binaryHandler.cache.getVal(path)
	if ok {
		info, err := os.Stat(path)
		if err == nil && info.ModTime().Before(cacheRet.modTime) {
			return cacheRet.path, nil
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("error when opening path:\n %w", err)
	}

	buf := make([]byte, 32)
	if _, err = f.Read(buf); err != nil && err != io.EOF {
		return "", fmt.Errorf("error when reading file:\n %w", err)
	}

	f.Close()

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
