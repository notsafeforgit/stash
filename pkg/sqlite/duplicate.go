package sqlite

import (
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sliceutil"
)

func parseDuplicateIDGroups(ids []string) [][]int {
	var ret [][]int
	for _, id := range ids {
		strIDs := strings.Split(id, ",")
		var intIDs []int
		for _, strID := range strIDs {
			if intID, err := strconv.Atoi(strID); err == nil {
				intIDs = sliceutil.AppendUnique(intIDs, intID)
			}
		}

		if len(intIDs) > 1 {
			ret = append(ret, intIDs)
		}
	}

	return ret
}

func duplicateIDs(groups [][]int) []int {
	var ret []int
	for _, group := range groups {
		ret = append(ret, group...)
	}
	return ret
}

func duplicateFindFilterAll() *models.FindFilterType {
	perPage := models.PerPageAll
	return &models.FindFilterType{
		PerPage: &perPage,
	}
}

func paginateDuplicateIDGroups(groups [][]int, findFilter *models.FindFilterType) ([][]int, int) {
	count := len(groups)
	if count == 0 {
		return nil, 0
	}
	if findFilter != nil && findFilter.IsGetAll() {
		return groups, count
	}

	if findFilter == nil {
		findFilter = &models.FindFilterType{}
	}

	pageSize := findFilter.GetPageSize()
	if pageSize == 0 {
		return nil, count
	}

	page := findFilter.GetPage()
	start := (page - 1) * pageSize
	if start >= count {
		return nil, count
	}

	end := start + pageSize
	if end > count {
		end = count
	}

	return groups[start:end], count
}
