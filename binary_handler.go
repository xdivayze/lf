package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

type Path string

type BinaryHandlerCache interface {
	getVal(path Path) Path
	setVal(key Path, val Path)
}

type BinaryHandlerCacheHashMap struct {
	cache map[Path]Path
}

func (binaryHandlerCache BinaryHandlerCacheHashMap) getVal(path Path) Path {
	return binaryHandlerCache.cache[path]
}

func (binaryHandlerCache *BinaryHandlerCacheHashMap) setVal(key Path, val Path) {
	binaryHandlerCache.cache[key] = val
}

type BinaryType interface {
	isThisBinary(path Path) (bool, error)
	binaryHandler(path Path) (string, error)
}

type BinaryHandler interface {
	handleBinary(path Path) (Path, error)
	checkIsBinary(path Path) (bool, error)

	// return true on change
	addBinaryType(binaryType BinaryType, overwite bool) bool
}

type SpecificHandlerEntry struct {
	magic   []byte
	handler func(Path) (Path, error)
}

type HeuristicBinaryHandler struct {
	cache            BinaryHandlerCache
	specificHandlers []SpecificHandlerEntry
}

// heristic handler
func (binaryHandler HeuristicBinaryHandler) handleBinary(path Path) (Path, error) {
	ret := binaryHandler.cache.getVal(path)
	if ret == "" {
		return ret, nil
	}

	f, err := os.Open(string(path))
	if err != nil {
		return "", fmt.Errorf("error when opening path:\n %w", err)
	}

	buf := make([]byte, 32)
	if _, err = f.Read(buf); err != nil && err != io.EOF {
		return "", fmt.Errorf("error when reading file:\n %w", err)
	}

	f.Close()

	done := make(chan int)
	defer close(done)
	idx := -1
	for i := range len(binaryHandler.specificHandlers) {
		go func() {
			if bytes.HasPrefix(buf, binaryHandler.specificHandlers[i].magic) {
				done <- i
			}
		}()
	}
	idx = <-done

	if idx == -1 {
		return "", fmt.Errorf("no handlers found for the file")
	}

	ret, err = binaryHandler.specificHandlers[idx].handler(path)
	if err != nil {
		return "", fmt.Errorf("error occured in the specific handler:\n %w", err)
	}

	if _, err = os.Stat(string(ret)); err != nil {
		return "", fmt.Errorf("the specific handler returned a bad path:\n%w", err)
	}

	binaryHandler.cache.setVal(path, ret)
	return ret, nil
}

func (binaryHandler *HeuristicBinaryHandler) addBinaryType(entry SpecificHandlerEntry, overwrite bool) bool {
	done := make(chan int)
	defer close(done)
	idx := -1
	for i := range len(binaryHandler.specificHandlers) {
		go func() {
			if bytes.Equal(binaryHandler.specificHandlers[i].magic, entry.magic) {
				done <- i
			}
		}()
	}

	idx = <-done
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

func (binaryHandler HeuristicBinaryHandler) checkIsBinary(path Path) (bool, error) {
	f, err := os.Open(string(path))
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
