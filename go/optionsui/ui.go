//go:build js && wasm

// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package optionsui defines the behavior underlying the user interface
// for the extension's options.
package optionsui

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"sort"
	"syscall/js"
	"time"

	"github.com/google/chrome-ssh-agent/go/dom"
	"github.com/google/chrome-ssh-agent/go/keys"
	"github.com/google/chrome-ssh-agent/go/keys/testdata"
	"github.com/google/go-cmp/cmp"
)

// UI implements the behavior underlying the user interface for the extension's
// options.
type UI struct {
	mgr              keys.Manager
	dom              *dom.DOM
	passphraseDialog js.Value
	passphraseInput  js.Value
	passphraseOk     js.Value
	passphraseCancel js.Value
	addButton        js.Value
	addDialog        js.Value
	addName          js.Value
	addKey           js.Value
	addOk            js.Value
	addCancel        js.Value
	removeDialog     js.Value
	removeName       js.Value
	removeYes        js.Value
	removeNo         js.Value
	errorText        js.Value
	keysData         js.Value
	keys             []*displayedKey
}

// New returns a new UI instance that manages keys using the supplied manager.
// domObj is the DOM instance corresponding to the document in which the Options
// UI is displayed.
func New(mgr keys.Manager, domObj *dom.DOM) *UI {
	result := &UI{
		mgr:              mgr,
		dom:              domObj,
		passphraseDialog: domObj.GetElement("passphraseDialog"),
		passphraseInput:  domObj.GetElement("passphrase"),
		passphraseOk:     domObj.GetElement("passphraseOk"),
		passphraseCancel: domObj.GetElement("passphraseCancel"),
		addButton:        domObj.GetElement("add"),
		addDialog:        domObj.GetElement("addDialog"),
		addName:          domObj.GetElement("addName"),
		addKey:           domObj.GetElement("addKey"),
		addOk:            domObj.GetElement("addOk"),
		addCancel:        domObj.GetElement("addCancel"),
		removeDialog:     domObj.GetElement("removeDialog"),
		removeName:       domObj.GetElement("removeName"),
		removeYes:        domObj.GetElement("removeYes"),
		removeNo:         domObj.GetElement("removeNo"),
		errorText:        domObj.GetElement("errorMessage"),
		keysData:         domObj.GetElement("keysData"),
	}

	// Populate keys on initial display
	result.dom.OnDOMContentLoaded(result.updateKeys)
	// Configure new key on click
	result.dom.OnClick(result.addButton, result.add)
	return result
}

// setError updates the UI to display the supplied error. If the supplied error
// is nil, then any displayed error is cleared.
func (u *UI) setError(err error) {
	// Clear any existing error
	u.dom.RemoveChildren(u.errorText)

	if err != nil {
		u.dom.AppendChild(u.errorText, u.dom.NewText(err.Error()), nil)
	}
}

// add configures a new key.  It displays a dialog prompting the user for a name
// and the corresponding private key.  If the user continues, the key is
// added to the manager.
func (u *UI) add(evt dom.Event) {
	u.promptAdd(func(name, privateKey string, ok bool) {
		if !ok {
			return
		}
		u.mgr.Add(name, privateKey, func(err error) {
			if err != nil {
				u.setError(fmt.Errorf("failed to add key: %v", err))
				return
			}

			u.setError(nil)
			u.updateKeys()
		})
	})
}

// promptAdd displays a dialog prompting the user for a name and private key.
// callback is invoked when the dialog is closed; the ok parameter indicates
// if the user clicked OK.
func (u *UI) promptAdd(callback func(name, privateKey string, ok bool)) {
	u.dom.OnClick(u.addOk, func(evt dom.Event) {
		n := u.dom.Value(u.addName)
		k := u.dom.Value(u.addKey)
		u.dom.SetValue(u.addName, "")
		u.dom.SetValue(u.addKey, "")
		u.addOk = u.dom.RemoveEventListeners(u.addOk)
		u.addCancel = u.dom.RemoveEventListeners(u.addCancel)
		u.dom.Close(u.addDialog)
		callback(n, k, true)
	})
	u.dom.OnClick(u.addCancel, func(evt dom.Event) {
		u.dom.SetValue(u.addName, "")
		u.dom.SetValue(u.addKey, "")
		u.addOk = u.dom.RemoveEventListeners(u.addOk)
		u.addCancel = u.dom.RemoveEventListeners(u.addCancel)
		u.dom.Close(u.addDialog)
		callback("", "", false)
	})
	u.dom.ShowModal(u.addDialog)
}

// load loads the key with the specified ID.  A dialog prompts the user for a
// passphrase if the private key is encrypted.
func (u *UI) load(id keys.ID, encrypted bool) {
	prompt := u.promptPassphrase
	if !encrypted {
		// Use a dummy callback that doesn't actually prompt if no
		// passphrase is required.
		prompt = func(callback func(passphrase string, ok bool)) {
			callback("", true)
		}
	}

	prompt(func(passphrase string, ok bool) {
		if !ok {
			return
		}
		u.mgr.Load(id, passphrase, func(err error) {
			if err != nil {
				u.setError(fmt.Errorf("failed to load key: %v", err))
				return
			}
			u.setError(nil)
			u.updateKeys()
		})
	})
}

// promptPassphrase displays a dialog prompting the user for a passphrase.
// callback is invoked when the dialog is closed; the ok parameter indicates
// if the user clicked OK.
func (u *UI) promptPassphrase(callback func(passphrase string, ok bool)) {
	u.dom.OnClick(u.passphraseOk, func(evt dom.Event) {
		p := u.dom.Value(u.passphraseInput)
		u.dom.SetValue(u.passphraseInput, "")
		u.passphraseOk = u.dom.RemoveEventListeners(u.passphraseOk)
		u.passphraseCancel = u.dom.RemoveEventListeners(u.passphraseCancel)
		u.dom.Close(u.passphraseDialog)
		callback(p, true)
	})
	u.dom.OnClick(u.passphraseCancel, func(evt dom.Event) {
		u.dom.SetValue(u.passphraseInput, "")
		u.passphraseOk = u.dom.RemoveEventListeners(u.passphraseOk)
		u.passphraseCancel = u.dom.RemoveEventListeners(u.passphraseCancel)
		u.dom.Close(u.passphraseDialog)
		callback("", false)
	})
	u.dom.ShowModal(u.passphraseDialog)
}

// unload unloads the specified key.
func (u *UI) unload(key *keys.LoadedKey) {
	u.mgr.Unload(key, func(err error) {
		if err != nil {
			u.setError(fmt.Errorf("failed to unload key: %v", err))
			return
		}
		u.setError(nil)
		u.updateKeys()
	})
}

// promptRemove displays a dialog prompting the user to confirm that a key
// should be removed. callback is invoked when the dialog is closed; the yes
// parameter indicates if the user clicked Yes.
func (u *UI) promptRemove(name string, callback func(yes bool)) {
	u.dom.RemoveChildren(u.removeName)
	u.dom.AppendChild(u.removeName, u.dom.NewText(name), nil)
	u.dom.OnClick(u.removeYes, func(evt dom.Event) {
		u.dom.RemoveChildren(u.removeName)
		u.removeYes = u.dom.RemoveEventListeners(u.removeYes)
		u.removeNo = u.dom.RemoveEventListeners(u.removeNo)
		u.dom.Close(u.removeDialog)
		callback(true)
	})
	u.dom.OnClick(u.removeNo, func(evt dom.Event) {
		u.dom.RemoveChildren(u.removeName)
		u.removeYes = u.dom.RemoveEventListeners(u.removeYes)
		u.removeNo = u.dom.RemoveEventListeners(u.removeNo)
		u.dom.Close(u.removeDialog)
		callback(false)
	})
	u.dom.ShowModal(u.removeDialog)
}

// remove removes the key with the specified ID.  A dialog prompts the user to
// confirm that the key should be removed.
func (u *UI) remove(id keys.ID, name string) {
	u.promptRemove(name, func(yes bool) {
		if !yes {
			return
		}

		u.mgr.Remove(id, func(err error) {
			if err != nil {
				u.setError(fmt.Errorf("failed to remove key: %v", err))
				return
			}

			u.setError(nil)
			u.updateKeys()
		})
	})
}

// displayedKey represents a key displayed in the UI.
type displayedKey struct {
	// ID is the unique ID corresponding to the key.
	ID keys.ID
	// Loaded indicates if the key is currently loaded.
	Loaded bool
	// Encrypted indicates if the private key is encrypted and requires a
	// passphrase to load. This field is only valid if the key is not
	// loaded.
	Encrypted bool
	// Name is the human-readable name assigned to the key.
	Name string
	// Type is the type of key (e.g., 'ssh-rsa').
	Type string
	// Blob is the public key material for the key.
	Blob string
}

func (d *displayedKey) LoadedKey() (*keys.LoadedKey, error) {
	blob, err := base64.StdEncoding.DecodeString(d.Blob)
	if err != nil {
		return nil, fmt.Errorf("failed to decode blob: %v", err)
	}

	l := &keys.LoadedKey{Type: d.Type}
	l.SetBlob(blob)
	return l, nil
}

// DisplayedKeys returns the keys currently displayed in the UI.
func (u *UI) displayedKeys() []*displayedKey {
	return u.keys
}

// buttonKind is the type of button displayed for a key.
type buttonKind int

const (
	// LoadButton indicates that the button loads the key into the agent.
	LoadButton buttonKind = iota
	// UnloadButton indicates that the button unloads the key from the
	// agent.
	UnloadButton
	// RemoveButton indicates that the button removes the key.
	RemoveButton
)

// buttonID returns the value of the 'id' attribute to be assigned to the HTML
// button.
func buttonID(kind buttonKind, id keys.ID) string {
	s := "unknown"
	switch kind {
	case LoadButton:
		s = "load"
	case UnloadButton:
		s = "unload"
	case RemoveButton:
		s = "remove"
	}
	return fmt.Sprintf("%s-%s", s, id)
}

// updateDisplayedKeys refreshes the UI to reflect the keys that should be
// displayed.
func (u *UI) updateDisplayedKeys() {
	u.dom.RemoveChildren(u.keysData)

	for _, k := range u.keys {
		k := k
		u.dom.AppendChild(u.keysData, u.dom.NewElement("tr"), func(row js.Value) {
			// Key name
			u.dom.AppendChild(row, u.dom.NewElement("td"), func(cell js.Value) {
				u.dom.AppendChild(cell, u.dom.NewElement("div"), func(div js.Value) {
					div.Set("className", "keyName")
					u.dom.AppendChild(div, u.dom.NewText(k.Name), nil)
				})
			})

			// Controls
			u.dom.AppendChild(row, u.dom.NewElement("td"), func(cell js.Value) {
				u.dom.AppendChild(cell, u.dom.NewElement("div"), func(div js.Value) {
					div.Set("className", "keyControls")
					if k.ID == keys.InvalidID {
						// We only control keys with a valid ID.
						return
					}

					if k.Loaded {
						// Unload button
						u.dom.AppendChild(div, u.dom.NewElement("button"), func(btn js.Value) {
							btn.Set("type", "button")
							btn.Set("id", buttonID(UnloadButton, k.ID))
							u.dom.AppendChild(btn, u.dom.NewText("Unload"), nil)
							u.dom.OnClick(btn, func(evt dom.Event) {
								l, err := k.LoadedKey()
								if err != nil {
									u.setError(fmt.Errorf("Failed to get loaded key: %v", err))
									return
								}
								u.unload(l)
							})
						})
					} else {
						// Load button
						u.dom.AppendChild(div, u.dom.NewElement("button"), func(btn js.Value) {
							btn.Set("type", "button")
							btn.Set("id", buttonID(LoadButton, k.ID))
							u.dom.AppendChild(btn, u.dom.NewText("Load"), nil)
							u.dom.OnClick(btn, func(evt dom.Event) {
								u.load(k.ID, k.Encrypted)
							})
						})
					}

					// Remove button
					u.dom.AppendChild(div, u.dom.NewElement("button"), func(btn js.Value) {
						btn.Set("type", "button")
						btn.Set("id", buttonID(RemoveButton, k.ID))
						u.dom.AppendChild(btn, u.dom.NewText("Remove"), nil)
						u.dom.OnClick(btn, func(evt dom.Event) {
							u.remove(k.ID, k.Name)
						})
					})
				})
			})

			// Type
			u.dom.AppendChild(row, u.dom.NewElement("td"), func(cell js.Value) {
				u.dom.AppendChild(cell, u.dom.NewElement("div"), func(div js.Value) {
					div.Set("className", "keyType")
					u.dom.AppendChild(div, u.dom.NewText(k.Type), nil)
				})
			})

			// Blob
			u.dom.AppendChild(row, u.dom.NewElement("td"), func(cell js.Value) {
				u.dom.AppendChild(cell, u.dom.NewElement("div"), func(div js.Value) {
					div.Set("className", "keyBlob")
					u.dom.AppendChild(div, u.dom.NewText(k.Blob), nil)
				})
			})
		})
	}
}

// mergeKeys merges configured and loaded keys to create a consolidated list
// of keys that should be displayed in the UI.
func mergeKeys(configured []*keys.ConfiguredKey, loaded []*keys.LoadedKey) []*displayedKey {
	// Build map of configured keys for faster lookup
	configuredMap := make(map[keys.ID]*keys.ConfiguredKey)
	for _, k := range configured {
		configuredMap[keys.ID(k.ID)] = k
	}

	var result []*displayedKey

	// Add all loaded keys. Keep track of the IDs that were detected as
	// being loaded.
	loadedIds := make(map[keys.ID]bool)
	for _, l := range loaded {
		// Gather basic fields we get for any loaded key.
		dk := &displayedKey{
			Loaded: true,
			Type:   l.Type,
			Blob:   base64.StdEncoding.EncodeToString(l.Blob()),
		}
		// Attempt to figure out if this is a key we loaded. If so, fill
		// in some additional information.  It is possible that a key with
		// a non-existent ID is loaded (e.g., it was removed while loaded);
		// in this case we claim we do not have an ID.
		if id := l.ID(); id != keys.InvalidID {
			if ak := configuredMap[id]; ak != nil {
				loadedIds[id] = true
				dk.ID = id
				dk.Name = ak.Name
			}
		}
		result = append(result, dk)
	}

	// Add all configured keys that are not loaded.
	for _, a := range configured {
		// Skip any that we already covered above.
		if loadedIds[keys.ID(a.ID)] {
			continue
		}

		result = append(result, &displayedKey{
			ID:        keys.ID(a.ID),
			Loaded:    false,
			Encrypted: a.Encrypted,
			Name:      a.Name,
		})
	}

	// Sort to ensure consistent ordering.
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Name < b.Name {
			return true
		}
		if a.Name > b.Name {
			return false
		}
		if a.Blob < b.Blob {
			return true
		}
		if a.Blob > b.Blob {
			return false
		}
		return a.ID < b.ID
	})

	return result
}

// updateKeys queries the manager for configured and loaded keys, then triggers
// UI updates to reflect the current state.
func (u *UI) updateKeys() {
	u.mgr.Configured(func(configured []*keys.ConfiguredKey, err error) {
		if err != nil {
			u.setError(fmt.Errorf("failed to get configured keys: %v", err))
			return
		}

		u.mgr.Loaded(func(loaded []*keys.LoadedKey, err error) {
			if err != nil {
				u.setError(fmt.Errorf("failed to get loaded keys: %v", err))
				return
			}

			u.setError(nil)
			u.keys = mergeKeys(configured, loaded)
			u.updateDisplayedKeys()
		})
	})
}

func lookupKey(disp []*displayedKey, name string) *displayedKey {
	for _, k := range disp {
		if k.Name == name {
			return k
		}
	}
	return nil
}

const (
	pollInterval = 100 * time.Millisecond
	pollTimeout  = 5 * time.Second
)

func poll(done func() bool) {
	timeout := time.Now().Add(pollTimeout)
	for time.Now().Before(timeout) {
		if done() {
			return
		}
		time.Sleep(pollInterval)
	}
}

// EndToEndTest runs a set of tests via the UI.  Failures are returned as a list
// of errors.
//
// No attempt is made to clean up from any intermediate state should the test
// fail.
func (u *UI) EndToEndTest() []error {
	var errs []error

	dom.Log("Generate random name to use for key")
	i, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to generate random number: %v", err))
		return errs // Remaining tests have hard dependency on key name.
	}
	keyName := fmt.Sprintf("e2e-test-key-%s", i.String())

	dom.Log("Configure a new key")
	u.dom.DoClick(u.addButton)
	u.dom.SetValue(u.addName, keyName)
	u.dom.SetValue(u.addKey, testdata.WithPassphrase.Private)
	u.dom.DoClick(u.addOk)

	dom.Log("Validate configured keys; ensure new key is present")
	var key *displayedKey
	poll(func() bool {
		key = lookupKey(u.displayedKeys(), keyName)
		return key != nil
	})
	if key == nil {
		errs = append(errs, fmt.Errorf("after added: failed to find key"))
		return errs // Remaining tests have hard dependency on configured key.
	}

	dom.Log("Load the new key")
	u.dom.DoClick(u.dom.GetElement(buttonID(LoadButton, key.ID)))
	u.dom.SetValue(u.passphraseInput, testdata.WithPassphrase.Passphrase)
	u.dom.DoClick(u.passphraseOk)

	dom.Log("Validate loaded keys; ensure new key is loaded")
	poll(func() bool {
		key = lookupKey(u.displayedKeys(), keyName)
		return key != nil && key.Loaded
	})
	if key != nil {
		if diff := cmp.Diff(key.Loaded, true); diff != "" {
			errs = append(errs, fmt.Errorf("after load: incorrect loaded state: %s", diff))
		}
		if diff := cmp.Diff(key.Type, testdata.WithPassphrase.Type); diff != "" {
			errs = append(errs, fmt.Errorf("after load: incorrect type: %s", diff))
		}
		if diff := cmp.Diff(key.Blob, testdata.WithPassphrase.Blob); diff != "" {
			errs = append(errs, fmt.Errorf("after load: incorrect blob: %s", diff))
		}
	} else if key == nil {
		errs = append(errs, fmt.Errorf("after load: failed to find key"))
	}

	dom.Log("Unload key")
	u.dom.DoClick(u.dom.GetElement(buttonID(UnloadButton, key.ID)))

	dom.Log("Validate loaded keys; ensure key is unloaded")
	poll(func() bool {
		key = lookupKey(u.displayedKeys(), keyName)
		return key != nil && !key.Loaded
	})
	if key != nil {
		if diff := cmp.Diff(key.Loaded, false); diff != "" {
			errs = append(errs, fmt.Errorf("after unload: incorrect loaded state: %s", diff))
		}
		if diff := cmp.Diff(key.Type, ""); diff != "" {
			errs = append(errs, fmt.Errorf("after unload: incorrect type: %s", diff))
		}
		if diff := cmp.Diff(key.Blob, ""); diff != "" {
			errs = append(errs, fmt.Errorf("after unload: incorrect blob: %s", diff))
		}
	} else if key == nil {
		errs = append(errs, fmt.Errorf("after unload: failed to find key"))
	}

	dom.Log("Remove key")
	u.dom.DoClick(u.dom.GetElement(buttonID(RemoveButton, key.ID)))
	u.dom.DoClick(u.removeYes)

	dom.Log("Validate configured keys; ensure key is removed")
	poll(func() bool {
		key = lookupKey(u.displayedKeys(), keyName)
		return key == nil
	})
	if key != nil {
		errs = append(errs, fmt.Errorf("after removed: incorrectly found key"))
	}

	return errs

}
