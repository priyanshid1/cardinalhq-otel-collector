// Copyright 2024 CardinalHQ, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package boxer

import (
	"io"
	"os"
	"sync"

	"github.com/hashicorp/go-multierror"
)

type FilesystemBuffer struct {
	sync.Mutex
	directory string
	openFiles map[int64]map[string]*os.File
	shutdown  bool
}

var (
	_ Buffer = (*FilesystemBuffer)(nil)
)

func NewFilesystemBuffer(directory string) *FilesystemBuffer {
	return &FilesystemBuffer{
		directory: directory,
		openFiles: make(map[int64]map[string]*os.File),
	}
}

func (b *FilesystemBuffer) Write(data *BufferRecord) error {
	b.Lock()
	defer b.Unlock()
	if b.shutdown {
		return ErrShutdown
	}

	if _, ok := b.openFiles[data.Interval]; !ok {
		b.openFiles[data.Interval] = make(map[string]*os.File)
	}

	if _, ok := b.openFiles[data.Interval][data.Scope]; !ok {
		file, err := os.CreateTemp(b.directory, "buffer-")
		if err != nil {
			return err
		}
		b.openFiles[data.Interval][data.Scope] = file
	}

	file := b.openFiles[data.Interval][data.Scope]
	_, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	return encodeToFile(file, data)
}

func (b *FilesystemBuffer) GetScopes(interval int64) (scopes []string, err error) {
	b.Lock()
	defer b.Unlock()
	if b.shutdown {
		return nil, ErrShutdown
	}

	files, ok := b.openFiles[interval]
	if !ok {
		return nil, nil
	}

	for scope := range files {
		scopes = append(scopes, scope)
	}

	return scopes, nil
}

func (b *FilesystemBuffer) GetIntervals() (intervals []int64, err error) {
	b.Lock()
	defer b.Unlock()
	if b.shutdown {
		return nil, ErrShutdown
	}

	for interval := range b.openFiles {
		intervals = append(intervals, interval)
	}
	return intervals, nil
}

func (b *FilesystemBuffer) ForEach(interval int64, scope string, fn ForEachFunc) error {
	b.Lock()
	defer b.Unlock()
	if b.shutdown {
		return ErrShutdown
	}

	file, ok := b.openFiles[interval][scope]
	if !ok {
		return nil
	}

	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	return iterate(file, fn)
}

func (b *FilesystemBuffer) CloseIntervalScope(interval int64, scope string) error {
	b.Lock()
	defer b.Unlock()
	if b.shutdown {
		return ErrShutdown
	}

	return unlockedCloseIntervalScope(b, interval, scope)
}

func unlockedCloseIntervalScope(b *FilesystemBuffer, interval int64, scope string) error {
	if _, ok := b.openFiles[interval]; !ok {
		return nil
	}
	if _, ok := b.openFiles[interval][scope]; !ok {
		return nil
	}
	file := b.openFiles[interval][scope]

	delete(b.openFiles[interval], scope)
	if len(b.openFiles[interval]) == 0 {
		delete(b.openFiles, interval)
	}

	var errs *multierror.Error
	errs = multierror.Append(errs, file.Close())
	errs = multierror.Append(errs, os.Remove(file.Name()))

	return errs.ErrorOrNil()
}

func (b *FilesystemBuffer) CloseInterval(interval int64) error {
	b.Lock()
	defer b.Unlock()
	if b.shutdown {
		return ErrShutdown
	}

	scopes, ok := b.openFiles[interval]
	if !ok {
		return nil
	}

	var errs *multierror.Error
	for _, file := range scopes {
		errs = multierror.Append(errs, unlockedCloseIntervalScope(b, interval, file.Name()))
	}
	delete(b.openFiles, interval)

	return errs.ErrorOrNil()
}

func (b *FilesystemBuffer) Shutdown() error {
	b.Lock()
	defer b.Unlock()
	if b.shutdown {
		return nil
	}
	b.shutdown = true

	var errs *multierror.Error

	for _, files := range b.openFiles {
		for _, file := range files {
			errs = multierror.Append(errs, file.Close())
		}
	}
	b.openFiles = nil

	return errs.ErrorOrNil()
}