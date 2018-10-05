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

	log "github.com/sirupsen/logrus"
)

// This file contains some basic functions when dealing with storages, for
// example the file system.

// FSMapper is a mapping between filesystem images and internal ids.
// It maps both, ids to images and images to ids, implementing a bijective
// mapping.
//
// A problem arises when we store for example histograms. Images may be deleted
// or new images added, thus the histograms stored (e.g. in an array) can't be
// directly used.
//
// FSMapper provides methods to keep such things synched.
//
// A mapper maps absolute paths to image ids (and vice versa). Meaning that
// the mapping can't just be transferred to another machine.
type FSMapper struct {
	NameMapping map[string]ImageID
	IDMapping   []string
}

// NewFSMapper creates a new mapper without any values (empty mappings).
// To create a mapper with content (i.e. reading a list of files from the
// filesystem) use CreateFSMapper.
func NewFSMapper() *FSMapper {
	return &FSMapper{
		NameMapping: make(map[string]ImageID),
		IDMapping:   nil,
	}
}

// Clear removes all registered images from the mappings.
func (m *FSMapper) Clear() {
	m.NameMapping = make(map[string]ImageID)
	m.IDMapping = nil
}

// Len returns the number of images stored in the mapper.
func (m *FSMapper) Len() int {
	return len(m.IDMapping)
}

// NumImages returns the number of images in the mapper as an ImageID.
// Values between 0 and NumImages - 1 are considered valid ids.
func (m *FSMapper) NumImages() ImageID {
	return ImageID(m.Len())
}

// GetID returns the id of an absolute image path. If the image wasn't
// registered the id will be invalid and the boolean false.
func (m *FSMapper) GetID(path string) (ImageID, bool) {
	// can't return the two value version directly
	if id, has := m.NameMapping[path]; has {
		return id, true
	}
	return -1, false
}

// GetPath returns the absolute path of the image with the given id. If no image
// with that id exists the returned path is the empty string and the boolean
// false.
func (m *FSMapper) GetPath(id ImageID) (string, bool) {
	if int(id) >= len(m.IDMapping) {
		return "", false
	}
	return m.IDMapping[id], true
}

// Register registers an image to the mapping and returns the id of the image.
// If an image with that path is already present in the storage the images will
// not get added.
//
// path must be an absolute path to an image resource, this is however not
// checked / enforced in Register. Ensure this before calling the function.
// The returned value is the newly assigned ImageID; however if the image is
// already present the second return value is false and the ImageID is not
// valid. So only if the returned bool is true the ImageID may be used.
//
// Register adjusts both mappings and is not safe for concurrent use.
func (m *FSMapper) Register(path string) (ImageID, bool) {
	if Debug {
		if !filepath.IsAbs(path) {
			log.WithField("path", path).Warn("fsMapper.Register called with relative path")
		}
	}
	_, exists := m.NameMapping[path]
	if exists {
		return -1, false
	}
	id := ImageID(len(m.IDMapping))
	m.NameMapping[path] = id
	m.IDMapping = append(m.IDMapping, path)
	if Debug {
		if len(m.IDMapping) != len(m.NameMapping) {
			log.WithFields(log.Fields{
				"idMappingLen":   len(m.IDMapping),
				"nameMappingLen": len(m.NameMapping),
			}).Warn("Invalid FSMapper state, no bijective mapping?")
		}
	}
	return id, true
}

// Load scans path for images supported by gomosaic.
//
// All files for which filter returns true will be registered to the mapping.
// If recursive is true also subdirectories of root will be scanned, otherwise
// only root is scanned.
//
// The filter function can be nil and is then set to JPGAndPNG. Any error while
// scanning the directory / the directories is returned together with nil.
//
// Note that if an error occurs it is still possible that some images were added
// to the storage.
func (m *FSMapper) Load(path string, recursive bool, filter SupportedImageFunc) error {
	if filter == nil {
		filter = JPGAndPNG
	}
	abs, absErr := filepath.Abs(path)
	switch {
	case absErr != nil:
		return absErr
	case recursive:
		return m.loadRecursive(abs, filter)
	default:
		return m.loadNonRecursive(abs, filter)
	}
}

func (m *FSMapper) loadNonRecursive(path string, filter SupportedImageFunc) error {
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !file.IsDir() && filter(filepath.Ext(file.Name())) {
			abs := filepath.Join(path, file.Name())
			if _, success := m.Register(abs); !success {
				log.WithField("path", abs).Info("Image already registered")
			}
		}
	}
	return nil
}

func (m *FSMapper) loadRecursive(path string, filter SupportedImageFunc) error {
	walkFunc := func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case !info.IsDir() && filter(filepath.Ext(path)):
			if _, success := m.Register(path); !success {
				log.WithField("path", path).Info("Image already registered")
			}
			return nil
		default:
			return nil
		}
	}
	if err := filepath.Walk(path, walkFunc); err != nil {
		return err
	}
	return nil
}

// CreateFSMapper creates an FSMapper containing images from the root directory.
// All files for which filter returns true will be registered to the mapping.
// If recursive is true also subdirectories of root will be scanned, otherwise
// only root is scanned.
//
// The filter function can be nil and is then set to JPGAndPNG. Any error while
// scanning the directory / the directories is returned together with nil.
//
// This is the same as creating a new FSMapper and then calling its load method.
// The only difference is that on an error nil will be returned (not a mapper
// containing some images).
func CreateFSMapper(root string, recursive bool, filter SupportedImageFunc) (*FSMapper, error) {
	res := NewFSMapper()
	if err := res.Load(root, recursive, filter); err != nil {
		return nil, err
	}
	return res, nil
}

// Gone returns images that are gone, i.e. images that are not registered
// in the mapper.
// This is useful for storages that store for example histograms. These storages
// can test which images are gone ("missing") from the filesystem.
//
// The result contains all images from paths that are not registered in the
// mapper.
//
// A storage can implement a "Mising" method by simply iterating over all
// elements in the mapper and testing if it has an entry for that.
func (m *FSMapper) Gone(paths []string) []string {
	res := make([]string, 0)
	for _, path := range paths {
		if _, has := m.NameMapping[path]; !has {
			res = append(res, path)
		}
	}
	return res
}

// FSImageDB implements ImageStorage. It uses images stored on the filesystem
// and opens them on demand.
// Files are retrieved from a FSMapper.
type FSImageDB struct {
	mapper *FSMapper
}

// NewFSImageDB returns a new data base given the filesystem mapper.
func NewFSImageDB(mapper *FSMapper) *FSImageDB {
	return &FSImageDB{mapper: mapper}
}

// NumImages returns the number of images in the database.
func (db *FSImageDB) NumImages() ImageID {
	return db.mapper.NumImages()
}

// LoadImage loads the image with the given id from the filesystem.
func (db FSImageDB) LoadImage(id ImageID) (image.Image, error) {
	file, hasFile := db.mapper.GetPath(id)
	if !hasFile {
		return nil, fmt.Errorf("Invalid image id: Not associated with an image %d", id)
	}
	r, openErr := os.Open(file)
	if openErr != nil {
		return nil, openErr
	}
	defer r.Close()
	img, _, decodeErr := image.Decode(r)
	return img, decodeErr
}

// LoadConfig loads the image configuration for the image with the given id from
// the filesystem.
func (db FSImageDB) LoadConfig(id ImageID) (image.Config, error) {
	file, hasFile := db.mapper.GetPath(id)
	if !hasFile {
		return image.Config{}, fmt.Errorf("Invalid image id: Not associated with an image %d", id)
	}
	r, openErr := os.Open(file)
	if openErr != nil {
		return image.Config{}, openErr
	}
	defer r.Close()
	config, _, decodeErr := image.DecodeConfig(r)
	return config, decodeErr
}
