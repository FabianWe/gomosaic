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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"

	"github.com/FabianWe/gomosaic"
	"github.com/google/uuid"

	log "github.com/sirupsen/logrus"
)

var (
	ErrAlreadyHandled = errors.New("Error was already handled")
)

const (
	VarKey   = "var"
	ValueKey = "value"
)

type Context struct {
	Storage     ConnectionStorage
	NumRoutines int
	CacheSize   int
}

func NewContext(storage ConnectionStorage) *Context {
	initialRoutines := runtime.NumCPU() * 2
	if initialRoutines <= 0 {
		// don't know if this can happen, better safe than sorry
		initialRoutines = 4
	}
	return &Context{
		Storage:     storage,
		NumRoutines: initialRoutines,
		CacheSize:   gomosaic.ImageCacheSize,
	}
}

type HandlerFunc func(context *Context, w http.ResponseWriter, r *http.Request) (interface{}, error)

func ToHTTPFunc(context *Context, handler HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if jsonData, err := handler(context, w, r); err != nil {
			if err != ErrAlreadyHandled {
				log.WithError(err).Error("Error in request")
				http.Error(w, "Internal Server Error", 500)
			}
		} else {
			jData, jErr := json.Marshal(jsonData)
			if jErr != nil {
				log.WithError(jErr).Error("Internal error: Can't marshal json")
				http.Error(w, "Internal Server Error", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(jData)
		}
	}
}

type JSONMap map[string]interface{}

func (m JSONMap) GetString(key string) (string, error) {
	val, has := m[key]
	if !has {
		return "", fmt.Errorf("Key not found: %s", key)
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("Entry for %s not of type string", key)
	}
	return str, nil
}

func (m JSONMap) GetInt(key string) (int, error) {
	val, has := m[key]
	if !has {
		return -1, fmt.Errorf("Key not found: %s", key)
	}
	asInt, ok := val.(int)
	if !ok {
		return -1, fmt.Errorf("Entry for %s not of type int", key)
	}
	return asInt, nil
}

func (m JSONMap) GetFloat(key string) (float64, error) {
	val, has := m[key]
	if !has {
		return -1.0, fmt.Errorf("Key not found: %s", key)
	}
	asFloat, ok := val.(float64)
	if !ok {
		return -1.0, fmt.Errorf("Entry for %s not of type float", key)
	}
	return asFloat, nil
}

func (m JSONMap) GetBool(key string) (bool, error) {
	val, has := m[key]
	if !has {
		return false, fmt.Errorf("Key not found: %s", key)
	}
	asBool, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("Entry for %s not of type bool", key)
	}
	return asBool, nil
}

func (m JSONMap) GetConnection() (ConnectionID, error) {
	str, lookupErr := m.GetString("connection")
	var id ConnectionID
	if lookupErr != nil {
		return id, lookupErr
	}
	uid, parseErr := uuid.Parse(str)
	if parseErr != nil {
		return id, parseErr
	}
	id = ConnectionID(uid)
	return id, nil
}

func ProcessRequest(w http.ResponseWriter, r *http.Request) (JSONMap, error) {
	if r.Body == nil {
		http.Error(w, "No request body given", 400)
		return nil, ErrAlreadyHandled
	}
	dec := json.NewDecoder(r.Body)
	m := make(map[string]interface{})
	err := dec.Decode(&m)
	if err != nil {
		http.Error(w,
			fmt.Sprintf("Invalid request, expected valid JSON, got: %s", err.Error()),
			400)
		return nil, ErrAlreadyHandled
	}
	return m, nil
}

// TODO change layout to be more clear...
type StateHandlerFunc func(state *State, context *Context, w http.ResponseWriter, jsonMap JSONMap) (interface{}, error)

func StateHandlerToHTTPFunc(context *Context, handler StateHandlerFunc) http.HandlerFunc {
	mosaicHandler := func(context *Context, w http.ResponseWriter, r *http.Request) (interface{}, error) {
		// first get json data
		// try to parse commands
		json, jsonErr := ProcessRequest(w, r)
		if jsonErr != nil {
			return nil, jsonErr
		}
		// get connection from json dict
		connectionID, connectionKeyErr := json.GetConnection()
		if connectionKeyErr != nil {
			http.Error(w, connectionKeyErr.Error(), 400)
			return nil, ErrAlreadyHandled
		}
		// get connection from context
		state, connErr := context.Storage.Get(connectionID)
		if connErr != nil {
			http.Error(w, connErr.Error(), 400)
			return nil, ErrAlreadyHandled
		}
		return handler(state, context, w, json)
	}
	return ToHTTPFunc(context, mosaicHandler)
}

func InitHandler(context *Context, w http.ResponseWriter, r *http.Request) (interface{}, error) {
	uuid, uuidErr := GenConnectionID()
	if uuidErr != nil {
		return nil, uuidErr
	}
	res := map[string]string{
		"connection": uuid.String(),
	}
	return res, nil
}

func GetVarHandler(state *State, context *Context, w http.ResponseWriter, jsonMap JSONMap) (interface{}, error) {
	res := map[string]interface{}{
		"cut":          state.cutMosaic,
		"jpeg-quality": state.jpgQuality,
		"interp":       gomosaic.InterPString(state.interP),
		"variety":      state.variety.DisplayString(),
		"best":         state.bestFit,
	}
	return res, nil
}

func SetVarHandler(state *State, context *Context, w http.ResponseWriter, jsonMap JSONMap) (interface{}, error) {
	// get variable
	varName, varErr := jsonMap.GetString(VarKey)
	if varErr != nil {
		http.Error(w, varErr.Error(), 400)
		return nil, ErrAlreadyHandled
	}
	var argErr error
	switch varName {
	case "cut":
		var newCut bool
		newCut, argErr = jsonMap.GetBool(ValueKey)
		if argErr == nil {
			state.cutMosaic = newCut
		}
	case "jpeg-quality":
		var newQuality int
		newQuality, argErr = jsonMap.GetInt(ValueKey)
		if argErr != nil {
			break
		}
		if newQuality < 1 || newQuality > 100 {
			argErr = fmt.Errorf("jpq-quality must be a value between 1 and 100, got %d", newQuality)
			break
		}
		state.jpgQuality = newQuality
	case "interp":
		var interpName string
		interpName, argErr = jsonMap.GetString(ValueKey)
		if argErr != nil {
			break
		}
		interP, interPParseErr := gomosaic.InterPFromString(interpName)
		if interPParseErr != nil {
			argErr = interPParseErr
			break
		}
		state.interP = interP
	case "variety":
		var varietyStr string
		varietyStr, argErr = jsonMap.GetString(ValueKey)
		if argErr != nil {
			break
		}
		variety, parseErr := gomosaic.ParseCMDVarietySelector(varietyStr)
		if parseErr != nil {
			argErr = parseErr
			break
		}
		state.variety = variety
	case "best":
		var val float64
		val, argErr = jsonMap.GetFloat(ValueKey)
		if argErr != nil {
			break
		}
		if val <= 0.0 || val > 100.0 {
			argErr = fmt.Errorf("best must be a value > 0 and ≤ 100, got %.5f", val)
			break
		}
		state.bestFit = val
	default:
		http.Error(w, fmt.Sprintf("Invalid variable name %s", varName), 400)
		return nil, ErrAlreadyHandled
	}
	if argErr != nil {
		http.Error(w, argErr.Error(), 400)
		return nil, ErrAlreadyHandled
	}
	res := map[string]bool{"success": true}
	return res, nil
}
