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

package pubsub

func NewBroadcaster[T any]() *Broadcaster[T] {
	return &Broadcaster[T]{subscribers: make(map[chan Event[T]]struct{})}
}

func (b *Broadcaster[T]) Subscribe(buffer int) chan Event[T] {
	ch := make(chan Event[T], buffer)

	b.mu.Lock()
	defer b.mu.Unlock()

	b.subscribers[ch] = struct{}{}
	return ch
}

func (b *Broadcaster[T]) Unsubscribe(ch chan Event[T]) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[ch]; ok {
		delete(b.subscribers, ch)
		close(ch)
	}
}

func (b *Broadcaster[T]) Publish(e Event[T]) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- e:
		default:
			// Channel is full, discard to avoid blocking
		}
	}
}
