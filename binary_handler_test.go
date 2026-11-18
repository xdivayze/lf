package main

import (
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandle(t *testing.T) {
	handler := HeuristicBinaryHandler{cache: &BinaryHandlerCacheHashMap{cache: make(map[Path]Path)}}

	cPdf := make(chan bool, 1)
	defer close(cPdf)
	magicPdf := []byte{0x25, 0x50, 0x44, 0x46, 0x2d}

	tmp := t.TempDir()
	pdfRet := filepath.Join(tmp, "pdfReturn0")
	f1, _ := os.Create(pdfRet)
	defer f1.Close()

	pdfHandler := func(path Path) (Path, error) {
		cPdf <- true
		return Path(pdfRet), nil
	}

	entryPdf := SpecificHandlerEntry{handler: pdfHandler, magic: magicPdf}
	handler.addBinaryType(entryPdf, true)

	cArb := make(chan bool, 1)
	defer close(cArb)
	magicArb := []byte{0x01, 0x02}

	arbRet := filepath.Join(tmp, "arbReturn0")
	f2, _ := os.Create(arbRet)
	defer f2.Close()

	arbHandler := func(path Path) (Path, error) {
		cArb <- true
		return Path(arbRet), nil
	}
	entryArb := SpecificHandlerEntry{handler: arbHandler, magic: magicArb}

	handler.addBinaryType(entryArb, true)

	path := filepath.Join(tmp, "random.bin")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	f.Write(magicPdf)

	var seed [32]byte
	r := rand.NewChaCha8(seed)

	if _, err = io.CopyN(f, r, 100<<10); err != nil {
		t.Fatal(err)
	}

	handler.handleBinary(Path(path))

	select {
	case <-cPdf:
	case <-cArb:
		t.Error("wrong handler called")

	}
	f.Close()

	pathArb := filepath.Join(tmp, "random_arb.bin")

	f, err = os.Create(pathArb)
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()

	f.Write(magicArb)

	if _, err = io.CopyN(f, r, 100<<10); err != nil {
		t.Fatal(err)
	}

	handler.handleBinary(Path(pathArb))

	select {
	case <-cPdf:
		t.Error("wrong handler called")
	case <-cArb:
	}

	oldPdfRet := strings.Clone(pdfRet)
	pdfRet = filepath.Join(tmp, "pdfReturn1")

	if ret, err := handler.handleBinary(Path(path)); string(ret) != oldPdfRet || err != nil {
		t.Errorf("cache no func, %s \n %s", ret, err.Error())
	}
}

func TestAddBinaryHandler(t *testing.T) {
	specificHandler := func(path Path) (Path, error) {
		return "", nil
	}

	magic := []byte{0x00}

	entry := SpecificHandlerEntry{handler: specificHandler, magic: magic}

	var handler HeuristicBinaryHandler

	if !handler.addBinaryType(entry, false) {
		t.Error("binary handler returned false for modification")
	}

	if handler.addBinaryType(entry, false) {
		t.Error("binary handler returned true for no-overwrite operation")
	}

	if !handler.addBinaryType(entry, true) {
		t.Error("binary handler returned false for overwrite operation")
	}
}

func TestCheckIsNotBinary(t *testing.T) {
	var handler HeuristicBinaryHandler

	tmp := t.TempDir()
	path := filepath.Join(tmp, "random.txt")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()

	data := "this is a non binary text file. it should pass as a non binary file in the test."
	f.WriteString(data)

	ret, err := handler.checkIsBinary(Path(path))
	if err != nil {
		t.Fatal(err)
	}

	if ret {
		t.Errorf("binary checker returned true for non binary file")
	}
}

func TestCheckIsBinary(t *testing.T) {
	var handler HeuristicBinaryHandler

	tmp := t.TempDir()
	path := filepath.Join(tmp, "random.bin")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()

	var seed [32]byte
	r := rand.NewChaCha8(seed)

	if _, err = io.CopyN(f, r, 100<<10); err != nil {
		t.Fatal(err)
	}

	ret, err := handler.checkIsBinary(Path(path))
	if err != nil {
		t.Fatal(err)
	}

	if !ret {
		t.Errorf("binary checker returned false for binary file")
	}
}
