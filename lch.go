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
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

// LCH is a sorted collection of global color histograms. Different schemes
// yield to a different number of LCHs, but for each image the same number of
// GCHs is computed.
//
// All histograms should be generated with the same k (sub-divisons).
type LCH struct {
	Histograms []*Histogram
}

// NewLCH creates a new LCH givent the histograms.
func NewLCH(histograms []*Histogram) *LCH {
	return &LCH{Histograms: histograms}
}

// Dist returns the distance between two LCHs parameterized by a HistogramMetric
// two compare the histograms. It returns
// |Δ(h1[1], h2[1])| + ... + |Δ(h1[n], h2[n])| if n is the number of GCHs
// of the LCH.
//
// If the LCHs are of different dimensions or the GCHs inside the LCHs are
// of different dimensions an error != nil is returned.
func (lch *LCH) Dist(other *LCH, delta HistogramMetric) (float64, error) {
	if len(lch.Histograms) != len(other.Histograms) {
		return -1.0, fmt.Errorf("Invalid LCH dimensions: %d != %d",
			len(lch.Histograms),
			len(other.Histograms))
	}

	res := make(chan float64, len(lch.Histograms))

	for i := range lch.Histograms {
		go func(index int) {
			res <- math.Abs(delta(lch.Histograms[index], other.Histograms[index]))
		}(i)
	}

	sum := 0.0
	for _ = range lch.Histograms {
		sum += <-res
	}

	return sum, nil
}

// RepairDistribution is used to ensure that distribution contains a matrix
// of numY rows and in each row numX columns. Usually this method does not do
// anything (and hopefully never will). But just to be sure we add it here.
// It will never decrease the number of rectangles, only increase if required.
//
// This function is usally only triggered in debug mode.
func RepairDistribution(distribution TileDivision, numX, numY int) TileDivision {
	y := len(distribution)
	if y != numY {
		log.WithFields(log.Fields{
			"expected": numY,
			"got":      y,
		}).Warn("FixedNumDivider returned distribution with wrong number of tiles (height)")
	}
	for j := y; j < numY; j++ {
		distribution = append(distribution, make([]image.Rectangle, numX))
	}
	for j := 0; j < numY; j++ {
		rects := distribution[j]
		x := len(rects)
		if x != numX {
			log.WithFields(log.Fields{
				"expected": numX,
				"got":      x,
				"row":      j,
			}).Warn("FixedNumDivider returned distribution with wrong number of tiles (width)")
		}
		for i := x; i < numX; i++ {
			rects = append(rects, image.Rectangle{})
		}
		distribution[j] = rects
	}
	return distribution
}

// LCHScheme returns the distribution of an image into sub images.
// Note that a sub image can be contained in multiple lists and not all lists
// must be of the same length. For example the four parts scheme: The first
// list could contain both top sub images. The western list would contain the
// bot left sub images. They both contain the to-left image.
//
// Schemes always return a fixed number of image lists.
type LCHScheme interface {
	GetParts(img image.Image) ([][]image.Image, error)
}

// GenLCH computes the LCHs an image. It uses the scheme to compute the image
// parts and then concurrently creates the GCHs for each list.
// k and normalize are defined as for the GCH method: k is the number of
// histogram sub-divisions and if normalize is true the GCHs are normalized.
func GenLCH(scheme LCHScheme, img image.Image, k uint, normalize bool) (*LCH, error) {
	dist, distErr := scheme.GetParts(img)
	if distErr != nil {
		return nil, distErr
	}
	res := make([]*Histogram, len(dist))
	// for each part compute GCH
	var wg sync.WaitGroup
	wg.Add(len(dist))
	for i, imgList := range dist {
		go func(index int, list []image.Image) {
			defer wg.Done()
			// compute histogram from image list
			res[index] = GenHistogramFromList(k, normalize, list...)
		}(i, imgList)
	}
	wg.Wait()
	return NewLCH(res), nil
}

// FourLCHScheme implements the scheme with four parts: north, west, south and
// east.
//
// It implements LCHScheme, the LCH contains the GCHs for the parts in the order
// described above.
type FourLCHScheme struct{}

// NewFourLCHScheme returns a new FourLCHScheme.
func NewFourLCHScheme() FourLCHScheme {
	return FourLCHScheme{}
}

// GetParts returns exactly four histograms (N, W, S, E).
func (s FourLCHScheme) GetParts(img image.Image) ([][]image.Image, error) {
	// first divide image into 4 blocks
	// setting cut to false means that these blocks are not necessarily of the
	// same size.
	divider := NewFixedNumDivider(2, 2, false)
	parts := divider.Divide(img.Bounds())
	if Debug {
		// if in debug mode check for errors while dividing the image
		parts = RepairDistribution(parts, 2, 2)
	}
	imageParts, partsErr := DivideImage(img, parts, 4)
	if partsErr != nil {
		return nil, fmt.Errorf("Error computing distribution for LCH: %s", partsErr.Error())
	}
	res := [][]image.Image{
		// north
		[]image.Image{imageParts[0][0], imageParts[0][1]},
		// west
		[]image.Image{imageParts[0][0], imageParts[1][0]},
		// south
		[]image.Image{imageParts[1][0], imageParts[1][1]},
		// east
		[]image.Image{imageParts[0][1], imageParts[1][1]},
	}
	return res, nil
}

// FiveLCHScheme implements the scheme with vie parts: north, west, south,
// east and center.
//
// It implements LCHScheme, the LCH contains the GCHs for the parts in the order
// described above.
type FiveLCHScheme struct{}

// NewFiveLCHScheme returns a new FourLCHScheme.
func NewFiveLCHScheme() FiveLCHScheme {
	return FiveLCHScheme{}
}

// GetParts returns exactly five histograms (N, W, S, E, C).
func (s FiveLCHScheme) GetParts(img image.Image) ([][]image.Image, error) {
	// first divide image into 9 blocks
	// setting cut to false means that these blocks are not necessarily of the
	// same size.
	divider := NewFixedNumDivider(3, 3, false)
	parts := divider.Divide(img.Bounds())
	if Debug {
		// if in debug mode check for errors while dividing the image
		parts = RepairDistribution(parts, 3, 3)
	}
	imageParts, partsErr := DivideImage(img, parts, 9)
	if partsErr != nil {
		return nil, fmt.Errorf("Error computing distribution for LCH: %s", partsErr.Error())
	}
	res := [][]image.Image{
		// north
		[]image.Image{imageParts[0][0], imageParts[0][1], imageParts[0][2]},
		// west
		[]image.Image{imageParts[0][0], imageParts[1][0], imageParts[2][0]},
		// south
		[]image.Image{imageParts[2][0], imageParts[2][1], imageParts[2][2]},
		// east
		[]image.Image{imageParts[0][2], imageParts[1][2], imageParts[2][2]},
		// center
		[]image.Image{imageParts[1][1]},
	}
	return res, nil
}

// CreateLCHs creates histograms for all images in the ids list and loads the
// images through the given storage.
// If you want to create all histograms for a given storage you can use
// CreateAllLCHs as a shortcut.
// It runs the creation of LCHs concurrently (how many go routines run
// concurrently can be controlled by numRoutines).
// k is the number of sub-divisons as described in the histogram type,
// If normalized is true the normalized histograms are computed.
// progress is a function that is called to inform about the progress,
// see doucmentation for ProgressFunc.
func CreateLCHs(scheme LCHScheme, ids []ImageID, storage ImageStorage, normalize bool,
	k uint, numRoutines int, progress ProgressFunc) ([]*LCH, error) {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	numImages := len(ids)
	// any error that occurs sets this variable (first error)
	// this is done later
	var err error

	res := make([]*LCH, numImages)
	jobs := make(chan int, BufferSize)
	errorChan := make(chan error, BufferSize)

	// workers
	for w := 0; w < numRoutines; w++ {
		go func() {
			for next := range jobs {
				image, imageErr := storage.LoadImage(ids[next])
				if imageErr != nil {
					errorChan <- imageErr
					continue
				}
				lch, lchErr := GenLCH(scheme, image, k, normalize)
				if lchErr != nil {
					errorChan <- lchErr
					continue
				}
				res[next] = lch
				errorChan <- nil
			}
		}()
	}

	// create jobs
	go func() {
		for i := 0; i < len(ids); i++ {
			jobs <- i
		}
		close(jobs)
	}()

	// read errors
	for i := 0; i < numImages; i++ {
		nextErr := <-errorChan
		if nextErr != nil && err == nil {
			err = nextErr
		}
		if progress != nil {
			progress(i)
		}
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CreateAllLCHs creates all lchs for images in the storage.
// It is a shortcut using CreateLCHs, see this documentation for details.
func CreateAllLCHs(scheme LCHScheme, storage ImageStorage, normalize bool,
	k uint, numRoutines int, progress ProgressFunc) ([]*LCH, error) {
	return CreateLCHs(scheme, IDList(storage), storage, normalize, k, numRoutines, progress)
}

// LCHStorage maps image ids to LCHs.
// By default the histograms of the LCHs should be normalized.
//
// Implementations must be safe for concurrent use.
type LCHStorage interface {
	// GetLCH returns the LCH for a previously registered ImageID.
	GetLCH(id ImageID) (*LCH, error)

	// Divisions returns the number of sub-divisions used in the gchs of an LCH.
	Divisions() uint

	// SchemeSize returns the number of gchs stored for each lch.
	SchemeSize() uint
}

// MemoryLCHStorage implements LCHStorage by keeping a list of LCHs in memory.
type MemoryLCHStorage struct {
	LCHs []*LCH
	K    uint
	Size uint
}

// NewMemoryLCHStorage returns a new memory LCH storage storing LCHs of size
// schemeSize with k sub-divisions. Capacity is the capacity of the underlying
// histogram array, negative values yield to a default capacity.
func NewMemoryLCHStorage(k, schemeSize uint, capacity int) *MemoryLCHStorage {
	if capacity < 0 {
		capacity = 100
	}
	return &MemoryLCHStorage{
		LCHs: make([]*LCH, 0, capacity),
		K:    k,
		Size: schemeSize,
	}
}

// GetLCH implements the LCHStorage interface function by returning the LCH
// on position id in the list.
// If id is not a valid position inside the the list an error is returned.
func (s *MemoryLCHStorage) GetLCH(id ImageID) (*LCH, error) {
	if int(id) < 0 || int(id) >= len(s.LCHs) {
		return nil, fmt.Errorf("LCH for id %d not registered", id)
	}
	return s.LCHs[id], nil
}

// Divisions returns the number of sub-divisions k.
func (s *MemoryLCHStorage) Divisions() uint {
	return s.K
}

// SchemeSize returns the number of GCHs stored for each LCH in the storage.
func (s *MemoryLCHStorage) SchemeSize() uint {
	return s.Size
}

// LCHFSEntry is used to store LCHs on the filesystem.
// It contains the path of the image the LCH was created for as well
// as the LCH data.
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
type LCHFSEntry struct {
	Path     string
	LCH      *LCH
	Checksum string
}

// NewLCHFSEntry returns a new entry with the given content.
func NewLCHFSEntry(path string, lch *LCH, checksum string) LCHFSEntry {
	return LCHFSEntry{
		Path:     path,
		LCH:      lch,
		Checksum: checksum,
	}
}

// LCHFSController is used to store LCHs (wrapped by LCHFSEntry) on the
// filesystem.
//
// It's the same idea as with HistogramFSController, see details there.
// Some of the functions implemented for HistogramFSController are not
// implemented here because they're not needed at the moment. But they could
// be implemented similar to those in HistogramFSController.
type LCHFSController struct {
	Entries []LCHFSEntry
	K       uint
	Size    uint
	Version string
}

// NewLCHFSController returns an empty file system controller with the given
// capacity. Too create a new file system controller initialized with some
// content use CreateLCHFSController.
func NewLCHFSController(k, schemeSize uint, capacity int) *LCHFSController {
	if capacity < 0 {
		capacity = 100
	}
	return &LCHFSController{
		Entries: make([]LCHFSEntry, 0, capacity),
		K:       k,
		Size:    schemeSize,
		Version: Version,
	}
}

// CreateLCHFSController creates a histogram filesystem controller given
// some input data.
// ids is the list of all image ids to be included in the controler, mapper
// is used to get the absolute path of an image (stored alongside the LCH
// data) and the storage is used to lookup the LCHs.
//
// If you want to create a fs controller with all ids from a storage you can use
// IDList to create a list of all ids.
func CreateLCHFSController(ids []ImageID, mapper *FSMapper, storage LCHStorage) (*LCHFSController, error) {
	res := NewLCHFSController(storage.Divisions(), storage.SchemeSize(), len(ids))
	for _, id := range ids {
		// lookup file name
		path, ok := mapper.GetPath(id)
		if !ok {
			return nil, fmt.Errorf("Can't retrieve path for image with id %d", id)
		}
		// lookup lch
		lch, lchErr := storage.GetLCH(id)
		if lchErr != nil {
			return nil, lchErr
		}
		res.Entries = append(res.Entries, NewLCHFSEntry(path, lch, ""))
	}
	return res, nil
}

// WriteGobFile writes the LCH to a file encoded gob format.
func (c *LCHFSController) WriteGobFile(path string) error {
	// just to be sure
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
func (c *LCHFSController) ReadGobFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := gob.NewDecoder(f)
	err = dec.Decode(c)
	return err
}

// WriteJSON writes the LCHs to  a file encoded in json format.
func (c *LCHFSController) WriteJSON(path string) error {
	// again, to be sure
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
func (c *LCHFSController) ReadJSONFile(path string) error {
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
func (c *LCHFSController) ReadFile(path string) error {
	ext := filepath.Ext(path)
	ext = strings.ToLower(ext)
	switch ext {
	case ".json":
		return c.ReadJSONFile(path)
	case ".gob":
		return c.ReadGobFile(path)
	default:
		return fmt.Errorf("Unkown file extension for LCH file: %s. Should be \".json\" or \".gob\"", ext)
	}
}

// WriteFile writes the content of the controller to a file depending on the
// file extension hich must be either .json or .gob.
func (c *LCHFSController) WriteFile(path string) error {
	ext := filepath.Ext(path)
	ext = strings.ToLower(ext)
	switch ext {
	case ".json":
		return c.WriteJSON(path)
	case ".gob":
		return c.WriteGobFile(path)
	default:
		return fmt.Errorf("Unkown file extension for LCH file: %s. Should be \".json\" or \".gob\"", ext)
	}
}

// Map computes the mapping filename ↦ lch. That is useful sometimes,
// especially when computing the diff between this and an FSMapper.
func (c *LCHFSController) Map() map[string]*LCH {
	res := make(map[string]*LCH, len(c.Entries))
	for _, entry := range c.Entries {
		res[entry.Path] = entry.LCH
	}
	return res
}

// LCHFileName returns the proposed filename for a file containing lchs.
// When saving LCHFSController instances (that's the type used for storing
// GCHs) the file should be saved by this file name.
// The scheme is "lch-scheme-k.(gob|json)".
// k is the value as defined in histogram and ext is the extension (gob for
// gob encoded files and json for json encoded files). Scheme is the scheme
// size, currently implemented are two parting techniques. This naming is
// ambiguous (someone could come up with another technique to build 5 blocks)
// but that should be well enough.
//
// For example LCHs with 8 sub-divions encoded as json with the 5 parts scheme
// would be stored in a file "lch-5-8.json".
func LCHFileName(k, schemeSize uint, ext string) string {
	if strings.HasPrefix(ext, ".") {
		ext = ext[1:]
	}
	return fmt.Sprintf("lch-%d-%d.%s", k, schemeSize, ext)
}

// MemLCHStorageFromFSMapper creates a new memory LCH storage that contains
// an entry MemLCHStorageFromFSMapper each image described by the filesystem mapper.
// If no lch for an image is found an error is returned.
//
// HistMap is the map as computed by the Map() function of the LCH
// controller. It is an argument to avoid multiple compoutations of it if used
// more often. Just set it to nil and it will be computed with the map function.
func MemLCHStorageFromFSMapper(mapper *FSMapper, fileContent *LCHFSController,
	lchMap map[string]*LCH) (*MemoryLCHStorage, error) {
	if lchMap == nil {
		lchMap = fileContent.Map()
	}
	res := NewMemoryLCHStorage(fileContent.K, fileContent.Size, mapper.Len())
	// now add each lch to the result, if no lch exists return an error
	for _, imagePath := range mapper.IDMapping {
		// lookup
		if lch, has := lchMap[imagePath]; has {
			res.LCHs = append(res.LCHs, lch)
			// k not stored, so we don't do the check as for histograms
		} else {
			return nil, fmt.Errorf("No LCH for image \"%s\" found", imagePath)
		}
	}
	return res, nil
}
