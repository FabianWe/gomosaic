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
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

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

// CreateFSMapper creates an FSMapper containing images from the root directory.
// All files for which filter returns true will be registered to the mapping.
// If recursive is true also subdirectories of root will be scanned, otherwise
// only root is scanned.
//
// The filter function can be nil and is then set to JPGAndPNG. Any error while
// scanning the directory / the directories is returned together with nil.
func CreateFSMapper(root string, recursive bool, filter SupportedImageFunc) (*FSMapper, error) {
	if filter == nil {
		filter = JPGAndPNG
	}
	root, absErr := filepath.Abs(root)
	switch {
	case absErr != nil:
		return nil, absErr
	case recursive:
		return createFSMapperRecursive(root, filter)
	default:
		return createFSMapperNonRecursive(root, filter)
	}
}

func createFSMapperNonRecursive(root string, filter SupportedImageFunc) (*FSMapper, error) {
	result := NewFSMapper()
	files, err := ioutil.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !file.IsDir() && filter(filepath.Ext(file.Name())) {
			abs := filepath.Join(root, file.Name())
			if _, success := result.Register(abs); !success {
				log.WithField("path", abs).Info("Image already registered")
			}
		}
	}
	return result, nil
}

func createFSMapperRecursive(root string, filter SupportedImageFunc) (*FSMapper, error) {
	result := NewFSMapper()
	walkFunc := func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case !info.IsDir() && filter(filepath.Ext(path)):
			if _, success := result.Register(path); !success {
				log.WithField("path", path).Info("Image already registered")
			}
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

// HistogramFSEntry is used to store a histogram on the filesystem.
// It contains the path of the image the histogram was created for as well
// as the histogram data.
type HistogramFSEntry struct {
	Path      string
	Histogram *Histogram
}

// HistogramFSController is used to store histograms (wrapped by
// HistogramFSEntry) on the filesystem.
type HistogramFSController struct {
	Entries []HistogramFSEntry
	K       uint
}

// NewHistogramFSController creates an empty file system controller with the
// given capacity.
//
// To create a new file system controller initialized with some content use
// CreateHistFSController.
func NewHistogramFSController(capacity int, k uint) *HistogramFSController {
	if capacity < 0 {
		capacity = 100
	}
	return &HistogramFSController{Entries: make([]HistogramFSEntry, 0, capacity), K: k}
}

// CreateHistFSController creates a histogram filesystem controller given
// some input data.
// ids is the list of all image ids to be included in the controler, mapper
// is used to get the absolute path of an image (stored alongside the histogram
// data) and the storage is used to lookup the histograms.
//
// If you want to create a fs controller with all ids from a storage you can use
// IDList to create a list of all ids.
func CreateHistFSController(ids []ImageID, mapper *FSMapper, storage HistogramStorage) (*HistogramFSController, error) {
	res := &HistogramFSController{
		Entries: make([]HistogramFSEntry, len(ids)),
		K:       storage.Divisions(),
	}
	for i, id := range ids {
		// lookup file name
		path, ok := mapper.GetPath(id)
		if !ok {
			return nil, fmt.Errorf("Can't retrieve path for image with id %d", id)
		}
		// lookup histogram
		hist, histErr := storage.GetHistogram(id)
		if histErr != nil {
			return nil, histErr
		}
		res.Entries[i] = HistogramFSEntry{Path: path, Histogram: hist}
	}
	return res, nil
}

// WriteGobFile writes the histograms to a file encoded gob format.
func (c *HistogramFSController) WriteGobFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	err = enc.Encode(c)
	return err
}

// ReadGobFile reads the content of the controller from the specified file.
// The file must be encoded in gob.
func (c *HistogramFSController) ReadGobFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := gob.NewDecoder(f)
	err = dec.Decode(c)
	return err
}

// WiteJSONFile writes the histograms to  a file encoded in json format.
func (c *HistogramFSController) WiteJSONFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	err = enc.Encode(c)
	return err
}

// ReadJSONFile reads the content of the controller from the specified file.
// The file must be encoded in json.
func (c *HistogramFSController) ReadJSONFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	err = dec.Decode(c)
	return err
}

// CheckData is used to verify (parts) of the controller data. It tests if
// the controller is defined for the same k as the argument k (tested only if
// checK is true). If you don't want to check k just set checK to false and k
// to some arbitrary value. It also checks if each histogram in the controler
// is defined for the same k (the k defined in the controller). If
// checkNormalized is set it also checks if each histogram only contains values
// between 0 and 1.
//
// This method should not be used in production code because it's rather slow,
// but it's useful for debugging.
//
// If the returned error is nil the check passed, otherwise an error != nil is
// returned describing all failed tests.
//
// Usually we deal with incorrectly stored files during mosaic generation:
// If there is an error with one of the histogram ojbects (wrong k) the
// metrics return an error. If somehow not-normalized histograms are stored
// the error is not detected, it should just lead to weird results.
func (c *HistogramFSController) CheckData(k uint, checkK bool, checkNormalized bool) error {
	errs := make([]string, 0)
	if checkK && c.K != k {
		errs = append(errs, fmt.Sprintf("Controller stores entries with k = %d, expected k = %d", c.K, k))
	}
	for _, entry := range c.Entries {
		histK := entry.Histogram.K
		if c.K != histK {
			errs = append(errs, fmt.Sprintf("Error in histogram for %s: Expected histogram with k = %d, got k = %d", entry.Path, c.K, histK))
		}
		histEntries := entry.Histogram.Entries
		if uint(len(histEntries)) != (histK * histK * histK) {
			errs = append(errs, fmt.Sprintf("Error in histogram for %s: Expected histogram of size %d, got size %d", entry.Path, (histK*histK*histK), len(histEntries)))
		}
		if checkNormalized {
			for _, value := range histEntries {
				if value < 0.0 || value > 1.0 {
					errs = append(errs, fmt.Sprintf("Error in histogram for %s: Found histogram entry %.2f", entry.Path, value))
				}
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "\n"))
}

// GCHFileName returns the proposed filename for a file containing global
// color histograms.
// When saving HistogramFSController instances (that's the type used for storing
// GCHs) the file should be saved by this file name.
// The scheme is "gch-k.(gob|json)".
// k is the value as defined in histogram and ext is the extension (gob for
// gob encoded files and json for json encoded files).
//
// For example histograms with 8 sub-divions encoded as json would be stored in
// a file "gch-8.json".
//
// Using this scheme makes it easier to find the precomputed data.
func GCHFileName(k uint, ext string) string {
	return fmt.Sprintf("gch-%d.%s", k, ext)
}

// MemoryHistStorage implements HistogramStorage by keeping a list of histograms
// in memory.
type MemoryHistStorage struct {
	Histograms []*Histogram
	K          uint
}

func NewMemoryHistStorage(k uint, capacity int) *MemoryHistStorage {
	if capacity < 0 {
		capacity = 100
	}
	return &MemoryHistStorage{
		Histograms: make([]*Histogram, 0, capacity),
		K:          k,
	}
}

func (s *MemoryHistStorage) GetHistogram(id ImageID) (*Histogram, error) {
	if int(id) >= len(s.Histograms) {
		return nil, fmt.Errorf("Histogram for id %d not registered", id)
	}
	return s.Histograms[id], nil
}

func (s *MemoryHistStorage) Divisions() uint {
	return s.K
}
