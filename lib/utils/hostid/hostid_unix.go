//go:build !windows

// Teleport
// Copyright (C) 2024 Gravitational, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package hostid

import (
	"errors"
	"io/fs"
	"time"

	"github.com/google/renameio/v2"
	"github.com/google/uuid"
	"github.com/gravitational/trace"

	"github.com/gravitational/teleport/lib/utils"
)

// WriteFile writes host UUID into a file
func WriteFile(dataDir string, id string) error {
	err := renameio.WriteFile(GetPath(dataDir), []byte(id), 0o400)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			//do not convert to system error as this loses the ability to compare that it is a permission error
			return trace.Wrap(err)
		}
		return trace.ConvertSystemError(err)
	}
	return nil
}

// ReadOrCreateFile looks for a hostid file in the data dir. If present,
// returns the UUID from it, otherwise generates one
func ReadOrCreateFile(dataDir string) (string, error) {
	hostUUIDFileLock := GetPath(dataDir) + ".lock"
	const iterationLimit = 3

	for i := 0; i < iterationLimit; i++ {
		if read, err := ReadFile(dataDir); err == nil {
			return read, nil
		} else if !trace.IsNotFound(err) {
			return "", trace.Wrap(err)
		}

		// Checking error instead of the usual uuid.New() in case uuid generation
		// fails due to not enough randomness. It's been known to happen happen when
		// Teleport starts very early in the node initialization cycle and /dev/urandom
		// isn't ready yet.
		rawID, err := uuid.NewRandom()
		if err != nil {
			return "", trace.BadParameter("" +
				"Teleport failed to generate host UUID. " +
				"This may happen if randomness source is not fully initialized when the node is starting up. " +
				"Please try restarting Teleport again.")
		}

		writeFile := func(potentialID string) (string, error) {
			unlock, err := utils.FSTryWriteLock(hostUUIDFileLock)
			if err != nil {
				return "", trace.Wrap(err)
			}
			defer unlock()

			if read, err := ReadFile(dataDir); err == nil {
				return read, nil
			} else if !trace.IsNotFound(err) {
				return "", trace.Wrap(err)
			}

			if err := WriteFile(dataDir, potentialID); err != nil {
				return "", trace.Wrap(err)
			}

			return potentialID, nil
		}

		id, err := writeFile(rawID.String())
		if err != nil {
			if errors.Is(err, utils.ErrUnsuccessfulLockTry) {
				time.Sleep(10 * time.Millisecond)
				continue
			}

			return "", trace.Wrap(err)
		}

		return id, nil
	}

	return "", trace.LimitExceeded("failed to obtain host uuid")
}
