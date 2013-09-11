package main

import (
	"errors"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type File struct {
	Url      *url.URL
	Mtime    time.Time
	FileFunc func() (reader io.ReadCloser, err error)
}

func (f File) ReadAll() (content []byte, err error) {
	reader, err := f.FileFunc()
	if err != nil {
		return
	}
	return ioutil.ReadAll(reader)
}

func Sync(from, to string, lookup RegistryFn) (chan File, chan error) {
	files := make(chan File)
	errs := make(chan error)

	fromUri, err := url.Parse(from)
	if err != nil {
		errs <- err
		return nil, errs
	}

	go func() {
		todos := []File{File{Url: fromUri}}
		for i := 0; i < len(todos); i++ {
			todo := todos[i]

			h := lookup(todo)
			if h == nil {
				errs <- errors.New("Cannot Sync: " + todo.Url.String())
				continue
			}

			hfiles := make(chan File)
			finish := make(chan bool)
			go func(f chan bool) {
				h.Files(todo, hfiles, errs)
				f <- true
			}(finish)

		LOOP:
			for {
				select {
				case <-finish:
					break LOOP
				case f := <-hfiles:
					if f.FileFunc == nil {
						todos = append(todos, f)
					} else {
						f.Url.Path = filepath.Join(to, f.Url.Path)
						err = Local(f)
						if err != nil {
							errs <- err
						} else {
							files <- f
						}
					}
				}
			}
		}
		close(files)
		close(errs)
	}()
	return files, errs
}

func removeNanoseconds(in time.Time) time.Time {
	return time.Date(
		in.Year(),
		in.Month(),
		in.Day(),
		in.Hour(),
		in.Minute(),
		in.Second(),
		0, in.Location())
}

func Local(file File) (err error) {
	st, err := os.Stat(file.Url.Path)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	if st == nil || file.Mtime.After(st.ModTime()) {
		err = os.MkdirAll(filepath.Dir(file.Url.Path), os.ModeDir|os.ModePerm)
		if err != nil {
			return err
		}
		osfile, err := os.Create(file.Url.Path)
		if err != nil {
			return err
		}
		defer osfile.Close()
		r, err := file.FileFunc()
		if err != nil {
			return err
		}
		_, err = io.Copy(osfile, r)
		if err != nil {
			return err
		}
		os.Chtimes(file.Url.Path, file.Mtime, file.Mtime)

		osfile.Sync()
	}
	return
}