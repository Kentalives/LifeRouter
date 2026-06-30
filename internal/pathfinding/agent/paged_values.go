// Copyright 2026 Kentalives
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package agent

import (
	"fmt"
	"sync"

	"github.com/kamstrup/intmap"

	"github.com/Kentalives/LifeRouter/internal/grid"
)

const (
	valuePageShift = 10
	valuePageSize  = 1 << valuePageShift
	valuePageMask  = valuePageSize - 1
	valuePageWords = valuePageSize / 64
)

type valuePage struct {
	g, rhs     [valuePageSize]grid.Cost
	gPresent   [valuePageWords]uint64
	rhsPresent [valuePageWords]uint64
}

var valuePagePool sync.Pool

// pagedValues stores sparse g/rhs D* Lite values without allocating a full map
// entry per visited cell. Dense mode indexes pages directly; sparse mode keeps a
// page directory for large worlds where only a small area is touched.
type pagedValues struct {
	sparse       bool
	pageCount    int
	densePages   []*valuePage
	sparsePages  *intmap.Map[int, *valuePage]
	activePages  []*valuePage
	gLen, rhsLen int
}

func (s *pagedValues) configure(worldSize int, sparse bool) {
	if len(s.activePages) != 0 {
		s.clear()
	}
	s.sparse = sparse

	pageCount := (worldSize + valuePageMask) >> valuePageShift
	s.pageCount = pageCount
	if sparse {
		s.densePages = nil
		if s.sparsePages == nil {
			s.sparsePages = intmap.New[int, *valuePage](min(pageCount, 16))
		}
		return
	}

	s.sparsePages = nil
	if cap(s.densePages) < pageCount {
		s.densePages = make([]*valuePage, pageCount)
	} else {
		s.densePages = s.densePages[:pageCount]
		clear(s.densePages)
	}
}

func (s *pagedValues) clear() {
	for _, page := range s.activePages {
		clear(page.gPresent[:])
		clear(page.rhsPresent[:])
		valuePagePool.Put(page)
	}
	s.activePages = s.activePages[:0]
	s.gLen = 0
	s.rhsLen = 0

	if s.sparsePages != nil {
		s.sparsePages.Clear()
	}
	clear(s.densePages)
}

func (s *pagedValues) page(index int) *valuePage {
	if index < 0 || index >= s.pageCount {
		return nil
	}
	if s.sparse {
		page, _ := s.sparsePages.Get(index)
		return page
	}
	if index >= len(s.densePages) {
		return nil
	}
	return s.densePages[index]
}

func (s *pagedValues) ensurePage(index int) *valuePage {
	if page := s.page(index); page != nil {
		return page
	}
	if index < 0 || index >= s.pageCount {
		panic(fmt.Sprintf("agent: paged value index %d outside page directory", index))
	}

	page, _ := valuePagePool.Get().(*valuePage)
	if page == nil {
		page = &valuePage{}
	}
	s.activePages = append(s.activePages, page)
	if s.sparse {
		s.sparsePages.Put(index, page)
	} else {
		s.densePages[index] = page
	}
	return page
}

// splitValueIndex maps a global cell index to a page slot and presence bit.
func splitValueIndex(index grid.GlobalIdx) (page, offset, word int, bit uint64) {
	valueIndex := int(index)
	offset = valueIndex & valuePageMask
	return valueIndex >> valuePageShift, offset, offset >> 6, uint64(1) << (offset & 63)
}

func (s *pagedValues) getG(index grid.GlobalIdx) grid.Cost {
	if index < 0 {
		return grid.UNREACHABLE_COST
	}
	pageIndex, offset, word, bit := splitValueIndex(index)
	page := s.page(pageIndex)
	if page == nil || page.gPresent[word]&bit == 0 {
		return grid.UNREACHABLE_COST
	}
	return page.g[offset]
}

func (s *pagedValues) getRhs(index grid.GlobalIdx) grid.Cost {
	if index < 0 {
		return grid.UNREACHABLE_COST
	}
	pageIndex, offset, word, bit := splitValueIndex(index)
	page := s.page(pageIndex)
	if page == nil || page.rhsPresent[word]&bit == 0 {
		return grid.UNREACHABLE_COST
	}
	return page.rhs[offset]
}

func (s *pagedValues) get(index grid.GlobalIdx) (g, rhs grid.Cost) {
	if index < 0 {
		return grid.UNREACHABLE_COST, grid.UNREACHABLE_COST
	}
	pageIndex, offset, word, bit := splitValueIndex(index)
	page := s.page(pageIndex)
	if page == nil {
		return grid.UNREACHABLE_COST, grid.UNREACHABLE_COST
	}
	if page.gPresent[word]&bit == 0 {
		g = grid.UNREACHABLE_COST
	} else {
		g = page.g[offset]
	}
	if page.rhsPresent[word]&bit == 0 {
		rhs = grid.UNREACHABLE_COST
	} else {
		rhs = page.rhs[offset]
	}
	return g, rhs
}

func (s *pagedValues) setG(index grid.GlobalIdx, value grid.Cost) {
	if index < 0 {
		panic(fmt.Sprintf("agent: negative paged value index %d", index))
	}
	pageIndex, offset, word, bit := splitValueIndex(index)
	page := s.page(pageIndex)
	if grid.IsUnreachable(value) {
		if page != nil && page.gPresent[word]&bit != 0 {
			page.gPresent[word] &^= bit
			s.gLen--
		}
		return
	}
	if page == nil {
		page = s.ensurePage(pageIndex)
	}
	if page.gPresent[word]&bit == 0 {
		page.gPresent[word] |= bit
		s.gLen++
	}
	page.g[offset] = value
}

func (s *pagedValues) setRhs(index grid.GlobalIdx, value grid.Cost) {
	if index < 0 {
		panic(fmt.Sprintf("agent: negative paged value index %d", index))
	}
	pageIndex, offset, word, bit := splitValueIndex(index)
	page := s.page(pageIndex)
	if grid.IsUnreachable(value) {
		if page != nil && page.rhsPresent[word]&bit != 0 {
			page.rhsPresent[word] &^= bit
			s.rhsLen--
		}
		return
	}
	if page == nil {
		page = s.ensurePage(pageIndex)
	}
	if page.rhsPresent[word]&bit == 0 {
		page.rhsPresent[word] |= bit
		s.rhsLen++
	}
	page.rhs[offset] = value
}

func (s *pagedValues) hasG(index grid.GlobalIdx) bool {
	if index < 0 {
		return false
	}
	pageIndex, _, word, bit := splitValueIndex(index)
	page := s.page(pageIndex)
	return page != nil && page.gPresent[word]&bit != 0
}
