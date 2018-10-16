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

package web

import (
	"errors"
	"sync"
	"time"

	"github.com/FabianWe/gomosaic"
	"github.com/google/uuid"
	"github.com/nfnt/resize"
)

type ConnectionID uuid.UUID

func GenConnectionID() (ConnectionID, error) {
	id, idErr := uuid.NewRandom()
	return ConnectionID(id), idErr
}

func (id ConnectionID) String() string {
	return uuid.UUID(id).String()
}

type State struct {
	created        time.Time
	lastConnection time.Time
	storage        gomosaic.ImageStorage
	gchStorage     gomosaic.HistogramStorage
	lchStorage     gomosaic.LCHStorage
	cutMosaic      bool
	jpgQuality     int
	interP         resize.InterpolationFunction
	variety        gomosaic.CmdVarietySelector
	bestFit        float64
}

func NewState() *State {
	now := time.Now().UTC()

	return &State{
		created:        now,
		lastConnection: now,
		storage:        nil,
		gchStorage:     nil,
		lchStorage:     nil,
		cutMosaic:      false,
		jpgQuality:     100,
		interP:         resize.Lanczos3,
		variety:        gomosaic.CmdVarietyNone,
		bestFit:        0.05,
	}
}

func (s *State) Expired(now time.Time, maxAge time.Duration) bool {
	age := now.Sub(s.lastConnection)
	return age >= maxAge
}

var (
	ErrConnNotFound = errors.New("Connection not found")
)

type ConnectionStorage interface {
	Get(conn ConnectionID) (*State, error)
	Set(conn ConnectionID, state *State) error
	Delete(conn ConnectionID) error
	Filter(maxAge time.Duration) error
}

type MemStorage struct {
	mutex   *sync.RWMutex
	connMap map[ConnectionID]*State
}

func NewMemStorage() *MemStorage {
	m := new(sync.RWMutex)
	connMap := make(map[ConnectionID]*State, 1000)
	return &MemStorage{
		mutex:   m,
		connMap: connMap,
	}
}

func (s *MemStorage) Get(conn ConnectionID) (*State, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	state, has := s.connMap[conn]
	if has {
		return state, nil
	}
	return nil, ErrConnNotFound
}

func (s *MemStorage) Set(conn ConnectionID, state *State) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.connMap[conn] = state
	return nil
}

func (s *MemStorage) Delete(conn ConnectionID) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.connMap, conn)
	return nil
}

func (s *MemStorage) Filter(maxAge time.Duration) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	now := time.Now().UTC()
	for id, state := range s.connMap {
		if state.Expired(now, maxAge) {
			delete(s.connMap, id)
		}
	}
	return nil
}
