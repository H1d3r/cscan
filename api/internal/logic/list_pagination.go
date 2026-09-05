package logic

import "go.mongodb.org/mongo-driver/bson"

const (
	defaultListPage     = 1
	defaultListPageSize = 10
	maxListPageSize     = 100
)

// normalizeListPage enforces the bounded pagination contract exposed by HTTP
// detail-list endpoints. It intentionally does not use model.NormalizePage,
// whose zero values retain the internal "unpaged" meaning.
func normalizeListPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = defaultListPage
	}
	if pageSize <= 0 {
		pageSize = defaultListPageSize
	} else if pageSize > maxListPageSize {
		pageSize = maxListPageSize
	}
	return page, pageSize
}

// appendBSONAndCondition adds a condition without overwriting an existing host,
// query, or technology expression already present in the filter.
func appendBSONAndCondition(filter bson.M, condition bson.M) {
	if existing, ok := filter["$and"]; ok {
		switch values := existing.(type) {
		case []bson.M:
			filter["$and"] = append(values, condition)
		case bson.A:
			filter["$and"] = append(values, condition)
		default:
			filter["$and"] = bson.A{existing, condition}
		}
		return
	}
	filter["$and"] = []bson.M{condition}
}
