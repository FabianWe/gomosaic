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
	"os"
	"path/filepath"
	"strings"
)

// This file contains functions and types for storing and retrieving global
// color histograms (GCHs).

// HistogramFSEntry is used to store a histogram on the filesystem.
// It contains the path of the image the histogram was created for as well
// as the histogram data.
//
// It also has a field checksum that is not used yet. Later it can be adjusted
// s.t. an histgram is stored together with the checksum (e.g. just plain md5
// encoded with e.g. base64) of the image the histogram was created for.
// This way we can test if the content of an image has changed, and thus
// the histogram became invalid. At the moment we don't recognize if an image
// has changed.
//
// This is however not supported at the moment. An empty string signals that
// no checksum was computed.
type HistogramFSEntry struct {
	Path      string
	Histogram *Histogram
	Checksum  string
}

// HistogramFSController is used to store histograms (wrapped by
// HistogramFSEntry) on the filesystem.
//
// Its intended use is to write an instance to a file (or whatever), making
// it possible to safe histograms connected to a named image (path).
//
// The idea is: Load histogram data from the filesystem and transform the
// histograms to HistogramStorage, maybe perform some tests if all images
// in the database are present in the controller.
//
// It also has a version field that is set to the Version variable when saving.
// This can be useful if the definition should ever change.
//
// See MissingEntries, AddtionalEntries and MemHistStorageFromFSMapper for
// some examples.
type HistogramFSController struct {
	Entries []HistogramFSEntry
	K       uint
	Version string
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
	return &HistogramFSController{
		Entries: make([]HistogramFSEntry, 0, capacity),
		K:       k,
	}
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
	c.Version = Version
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

// WriteJSON writes the histograms to  a file encoded in json format.
func (c *HistogramFSController) WriteJSON(path string) error {
	c.Version = Version
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

// ReadFile reads the content of the controller from the specified file.
// The read method depends on the file extension which must be either .json
// or .gob.
func (c *HistogramFSController) ReadFile(path string) error {
	ext := filepath.Ext(path)
	ext = strings.ToLower(ext)
	switch ext {
	case ".json":
		return c.ReadJSONFile(path)
	case ".gob":
		return c.ReadGobFile(path)
	default:
		return fmt.Errorf("Unkown file extension for GCH file: %s. Should be \".json\" or \".gob\"", ext)
	}
}

// WriteFile writes the content of the controller to a file depending on the
// file extension hich must be either .json or .gob.
func (c *HistogramFSController) WriteFile(path string) error {
	ext := filepath.Ext(path)
	ext = strings.ToLower(ext)
	switch ext {
	case ".json":
		return c.WriteJSON(path)
	case ".gob":
		return c.WriteGobFile(path)
	default:
		return fmt.Errorf("Unkown file extension for GCH file: %s. Should be \".json\" or \".gob\"", ext)
	}
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

// Map computes the mapping filename ↦ histogram. That is useful sometimes,
// especially when computing the diff between this and an FSMapper.
func (c *HistogramFSController) Map() map[string]*Histogram {
	res := make(map[string]*Histogram, len(c.Entries))
	for _, entry := range c.Entries {
		res[entry.Path] = entry.Histogram
	}
	return res
}

// MissingEntries computes the set of all images that are present in the mapping
// m but have no matching entry in the histogram.
//
// That is: For these images new histograms must be computed.
// HistMap is the map as computed by the Map() function. It is an argument to
// avoid multiple compoutations of it if used more often. Just set it to nil
// and it will be computed with the map function.
func (c *HistogramFSController) MissingEntries(m *FSMapper, histMap map[string]*Histogram) []string {
	if histMap == nil {
		histMap = c.Map()
	}
	res := make([]string, 0)
	for _, path := range m.IDMapping {
		// lookup in hist mapp
		if _, has := histMap[path]; !has {
			// not found, add to result
			res = append(res, path)
		}
	}
	return res
}

// AddtionalEntries computes all images files that are present in the histogram
// controller but not in the mapper. Usually that means that the image has been
// deleted and is no longer required.
func (c *HistogramFSController) AddtionalEntries(m *FSMapper) []string {
	res := make([]string, 0)
	for _, entry := range c.Entries {
		path := entry.Path
		if _, has := m.GetID(path); !has {
			res = append(res, path)
		}
	}
	return res
}

// Remove removes all entries from the controller whose path is in the paths
// element. Example usage: Use AddtionalEntries to compute histograms that are
// probably not required any more and then Remove them.
func (c *HistogramFSController) Remove(paths []string) {
	asSet := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		asSet[path] = struct{}{}
	}
	newSize := len(c.Entries) - len(paths)
	if newSize < 0 {
		newSize = 0
	}
	newEntries := make([]HistogramFSEntry, 0, newSize)
	for _, entry := range c.Entries {
		// test if path is in the elements to remove, if so don't append, otherwise
		// append to new entries
		if _, toRemove := asSet[entry.Path]; !toRemove {
			newEntries = append(newEntries, entry)
		}
	}
	c.Entries = newEntries
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
	if strings.HasPrefix(ext, ".") {
		ext = ext[1:]
	}
	return fmt.Sprintf("gch-%d.%s", k, ext)
}

// MemoryHistStorage implements HistogramStorage by keeping a list of histograms
// in memory.
type MemoryHistStorage struct {
	Histograms []*Histogram
	K          uint
}

// NewMemoryHistStorage returns a new memory histogram storage storing
// histograms with k sub-divisons. Capacity is the capacity of the underlying
// histogram array, negative values yield to a default capacity.
func NewMemoryHistStorage(k uint, capacity int) *MemoryHistStorage {
	if capacity < 0 {
		capacity = 100
	}
	return &MemoryHistStorage{
		Histograms: make([]*Histogram, 0, capacity),
		K:          k,
	}
}

// GetHistogram implements the HistogramStorage interface method by return
// the histogram on position id in the list.
// If id is not a valid position inside the the list an error is returned.
func (s *MemoryHistStorage) GetHistogram(id ImageID) (*Histogram, error) {
	if int(id) < 0 || int(id) >= len(s.Histograms) {
		return nil, fmt.Errorf("Histogram for id %d not registered", id)
	}
	return s.Histograms[id], nil
}

// Divisions returns the number of sub-divisions k.
func (s *MemoryHistStorage) Divisions() uint {
	return s.K
}

// TODO provide example sticking this all together

// MemHistStorageFromFSMapper creates a new memory histogram storage that
// contains an entry for each image described by the filesystem mapper.
// If no histogram for an image is found an error is returned. An error
// is also returned if there is an invalid histogram (wrong k, wrong size
// of entries).
//
// HistMap is the map as computed by the Map() function of the histogram
// controller. It is an argument to avoid multiple compoutations of it if used
// more often. Just set it to nil and it will be computed with the map function.
func MemHistStorageFromFSMapper(mapper *FSMapper, fileContent *HistogramFSController,
	histMap map[string]*Histogram) (*MemoryHistStorage, error) {
	// first create the mapping filename -> histogram
	if histMap == nil {
		histMap = fileContent.Map()
	}
	res := NewMemoryHistStorage(fileContent.K, mapper.Len())
	// now add each histogram to the result, if no histogram exists return
	// an error
	for _, imagePath := range mapper.IDMapping {
		// now look up the histogram
		if histogram, has := histMap[imagePath]; has {
			res.Histograms = append(res.Histograms, histogram)
			// this is just a check to be sure only legal histograms are saved
			k := histogram.K
			if k != fileContent.K {
				return nil, fmt.Errorf("Invalid histogram for image \"%s\": Illegal dimension: %d != %d",
					imagePath, histogram.K, fileContent.K)
			}
			if (k * k * k) != uint(len(histogram.Entries)) {
				return nil,
					fmt.Errorf("Invalid histogram for image \"%s\": Not the correct number of entries in histogram",
						imagePath)
			}
		} else {
			// return error
			return nil, fmt.Errorf("No histogram for image \"%s\" found", imagePath)
		}
	}
	return res, nil
}
