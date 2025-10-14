/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package simple

import (
	"sync"
)

type Store[T StoreResourceI] struct {
	mu sync.RWMutex

	// collections represent a list of objects indexed by a composed key.
	// For example, the pattern for a key could be: {group}/{version}/{resource}/{operation}
	collections map[string][]T
}
