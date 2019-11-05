/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

package types

// ErrorList is a Combination of a list of errors.
type ErrorList []error

// Error returns the detail error messages.
func (l ErrorList) Error() string {
	if len(l) == 0 {
		return ""
	}
	msg := l[0].Error()
	for i := 1; i < len(l); i++ {
		msg += "\n" + l[i].Error()
	}
	return msg
}
