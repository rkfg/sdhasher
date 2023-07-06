package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jessevdk/go-flags"
)

var params struct {
	Path       string `short:"p" description:"Path to the models directory" required:"true"`
	Input      string `short:"i" description:"Path to source cache.json file"`
	Output     string `short:"o" description:"Path to resulting cache.json file" required:"true"`
	MaxHashers int    `short:"m" description:"Max number of hashing tasks"`
}

type entry struct {
	MTime  MTime  `json:"mtime"`
	SHA256 string `json:"sha256"`
	path   string
}

type cache struct {
	Hashes       map[string]entry `json:"hashes"`
	HashesAddnet map[string]entry `json:"hashes-addnet,omitempty"`
}

type task struct {
	path string
	d    fs.DirEntry
}

type MTime float64

func (m MTime) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%.7f", m)), nil
}

func worker(t task) (*entry, error) {
	info, err := t.d.Info()
	mtime := float64(0)
	if err != nil {
		log.Printf("Error getting info for %s: %s", t.path, err)
		return nil, err
	} else {
		mtime = float64(info.ModTime().UnixNano())/1e9 + 1 // add one second margin because floats suck
	}
	log.Printf("Hashing %s", t.path)
	buf := [16384]byte{}
	h := sha256.New()
	h.Reset()
	f, err := os.Open(t.path)
	if err != nil {
		log.Printf("Error opening %s: %s", t.path, err)
		return nil, err
	}
	defer f.Close()
	n := 1
	for n > 0 {
		n, err = f.Read(buf[:])
		if n != 0 && err != nil {
			log.Printf("Error reading %s: %s", t.path, err)
			return nil, err
		}
		_, err := h.Write(buf[:n])
		if err != nil {
			log.Printf("Error hashing %s: %s", t.path, err)
			return nil, err
		}
	}
	hash := h.Sum(nil)
	return &entry{MTime: MTime(mtime), SHA256: fmt.Sprintf("%x", hash), path: t.path}, nil
}

func main() {
	_, err := flags.Parse(&params)
	if err != nil {
		os.Exit(1)
	}
	result := cache{Hashes: map[string]entry{}}
	if params.Input != "" {
		inf, err := os.Open(params.Input)
		if err != nil {
			log.Fatalf("Error opening cache file: %s", err)
		}
		err = json.NewDecoder(inf).Decode(&result)
		inf.Close()
		if err != nil {
			log.Fatalf("Error reading cache: %s", err)
		}
	}
	log.Printf("Processing %s", params.Path)
	taskChan := make(chan *task, 100)
	resultChan := make(chan *entry, 100)
	wg := sync.WaitGroup{}
	wgResult := sync.WaitGroup{}
	if params.MaxHashers == 0 {
		params.MaxHashers = runtime.NumCPU()
	}
	for i := 0; i < params.MaxHashers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskChan {
				e, err := worker(*t)
				if err == nil {
					resultChan <- e
				}
			}
		}()
	}
	wgResult.Add(1)
	go func() {
		defer wgResult.Done()
		for e := range resultChan {
			rel, err := filepath.Rel(params.Path, e.path)
			if err != nil {
				log.Printf("Error getting relative path: %s", err)
				continue
			}
			log.Printf("%s | %x", e.path, e.SHA256)
			rel = "checkpoint/" + rel
			result.Hashes[rel] = *e
		}
	}()
	knownFiles := map[string]struct{}{}
	for p, e := range result.Hashes {
		modelPath := filepath.Join(params.Path, strings.TrimPrefix(p, "checkpoint/"))
		fi, err := os.Stat(modelPath)
		if err != nil {
			log.Printf("Error accessing file %s: %s, removing cache entry", modelPath, err)
			delete(result.Hashes, p)
			continue
		}
		if fi.ModTime().Sub(time.Unix(int64(e.MTime), 0)) > time.Second*2 {
			log.Printf("File %s changed, rehashing...", modelPath)
			taskChan <- &task{path: modelPath, d: fs.FileInfoToDirEntry(fi)}
		}
		knownFiles[modelPath] = struct{}{}
	}
	filepath.WalkDir(params.Path, func(path string, d fs.DirEntry, err error) error {
		if d != nil && d.IsDir() {
			return nil
		}
		if err != nil {
			log.Printf("Error visiting %s: %s", path, err)
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".safetensors" && ext != ".ckpt" {
			return nil
		}
		if _, ok := knownFiles[path]; !ok {
			taskChan <- &task{path: path, d: d}
		}
		return nil
	})
	close(taskChan)
	wg.Wait()
	close(resultChan)
	wgResult.Wait()
	f, err := os.Create(params.Output)
	if err != nil {
		log.Fatalf("Error creating output file %s: %s", params.Output, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	err = enc.Encode(result)
	if err != nil {
		log.Fatalf("Error encoding result: %s", err)
	}
}
