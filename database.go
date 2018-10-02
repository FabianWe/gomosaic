// Copyright 2018 Fabian Wenzelmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gomosaic

import (
	"fmt"
	"image"
	"io/ioutil"
	"os"
	"path/filepath"
)

// FSImageDB implements ImageStorage. It uses images stored on the filesystem
// and opens them on demand.
// The paths are stored relative to a Root directory, thus to get the
// absolute path to an image... TODO
type FSImageDB struct {
	Root  string
	Paths []string
}

func (db *FSImageDB) GetPath(id ImageID) string {
	return filepath.Join(db.Root, db.Paths[id])
}

func NewFSImageDB(root string) *FSImageDB {
	return &FSImageDB{Root: root, Paths: nil}
}

func (db *FSImageDB) NumImages() ImageID {
	return ImageID(len(db.Paths))
}

func (db FSImageDB) LoadImage(id ImageID) (image.Image, error) {
	if id >= db.NumImages() {
		return nil, fmt.Errorf("Invalid image id: Not associated with an image %d", id)
	}
	// open file
	file := db.GetPath(id)
	r, openErr := os.Open(file)
	if openErr != nil {
		return nil, openErr
	}
	defer r.Close()
	img, _, decodeErr := image.Decode(r)
	return img, decodeErr
}

func (db FSImageDB) LoadConfig(id ImageID) (image.Config, error) {
	if id >= db.NumImages() {
		return image.Config{}, fmt.Errorf("Invalid image id: Not associated with an image %d", id)
	}
	// open
	file := db.GetPath(id)
	r, openErr := os.Open(file)
	if openErr != nil {
		return image.Config{}, openErr
	}
	defer r.Close()
	config, _, decodeErr := image.DecodeConfig(r)
	return config, decodeErr
}

// TODO deal with recursive
func GenFSDatabase(root string, recursive bool, filter SupportedImageFunc) (*FSImageDB, error) {
	root, absErr := filepath.Abs(root)
	if absErr != nil {
		return nil, absErr
	}
	if filter == nil {
		filter = JPGAndPNG
	}
	if recursive {
		return genFSDBRecursive(root, filter)
	}
	return genFSDBNonRecursive(root, filter)
}

func genFSDBRecursive(root string, filter SupportedImageFunc) (*FSImageDB, error) {
	result := NewFSImageDB(root)
	walkFunc := func(path string, info os.FileInfo, err error) error {

		switch {
		case err != nil:
			return err
		case !info.IsDir() && filter(filepath.Ext(path)):
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			result.Paths = append(result.Paths, rel)
			return nil
		default:
			return nil
		}
	}
	if err := filepath.Walk(root, walkFunc); err != nil {
		return nil, err
	}
	return result, nil
}

func genFSDBNonRecursive(root string, filter SupportedImageFunc) (*FSImageDB, error) {
	result := NewFSImageDB(root)
	files, err := ioutil.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !file.IsDir() && filter(filepath.Ext(file.Name())) {
			result.Paths = append(result.Paths, file.Name())
		}
	}
	return result, nil
}
