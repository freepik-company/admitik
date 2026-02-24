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

package keys

import "fmt"

func GVRKey(group, version, resource string) string {
	return fmt.Sprintf("%s/%s/%s", group, version, resource)
}

func GVROKey(group, version, resource, operation string) string {
	return fmt.Sprintf("%s/%s/%s/%s", group, version, resource, operation)
}

func GVRNNKey(group, version, resource, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", group, version, resource, namespace, name)
}
