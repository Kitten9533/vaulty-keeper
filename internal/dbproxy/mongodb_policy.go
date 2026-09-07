package dbproxy

import (
	"errors"
	"math"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type mongoNamespace struct {
	Database   string
	Collection string
}

// This is an explicit protocol policy, not a query engine or a data redactor.
// Backend roles and the caller's collection-type checks remain mandatory.
func mongoCheckCommand(doc bson.D) error {
	if !mongoPolicyCommandOK(doc) {
		return errors.New("MongoDB command rejected by proxy policy")
	}
	return nil
}

func mongoPolicyHas(words, word string) bool {
	for _, candidate := range strings.Fields(words) {
		if candidate == word {
			return true
		}
	}
	return false
}

func mongoPolicyGet(doc bson.D, key string) any {
	for _, e := range doc {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

func mongoPolicyFields(doc bson.D, allowed string) bool {
	seen := make(map[string]bool, len(doc))
	for _, e := range doc {
		if seen[e.Key] || !mongoPolicyHas(allowed, e.Key) {
			return false
		}
		seen[e.Key] = true
	}
	return true
}

func mongoPolicyPresent(doc bson.D, key string) bool {
	for _, e := range doc {
		if e.Key == key {
			return true
		}
	}
	return false
}

func mongoPolicyInt(value any) (int64, bool) {
	switch n := value.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) && n >= -9223372036854775808.0 && n < 9223372036854775808.0 && math.Trunc(n) == n {
			return int64(n), true
		}
	}
	return 0, false
}

func mongoPolicyRange(value any, min, max int64) bool {
	n, ok := mongoPolicyInt(value)
	return ok && n >= min && n <= max
}

func mongoPolicyDB(value any, admin bool) bool {
	name, ok := value.(string)
	if !ok || name == "" || len(name) > 63 || strings.ContainsAny(name, "/\\. \"$*<>:|?\x00") {
		return false
	}
	if strings.EqualFold(name, "local") || strings.EqualFold(name, "config") {
		return false
	}
	return !strings.EqualFold(name, "admin") || admin && name == "admin"
}

func mongoPolicyCollection(value any) bool {
	name, ok := value.(string)
	return ok && name != "" && len(name) <= 255 && !strings.ContainsAny(name, "$\x00") && !strings.HasPrefix(strings.ToLower(name), "system.") && !strings.HasPrefix(name, ".") && !strings.HasSuffix(name, ".") && !strings.Contains(name, "..")
}

func mongoPolicyField(value any) bool {
	name, ok := value.(string)
	return ok && name != "" && !strings.HasPrefix(name, "$") && !strings.ContainsRune(name, 0)
}

func mongoPolicySession(value any) bool {
	d, ok := value.(bson.D)
	if !ok || len(d) != 1 || d[0].Key != "id" {
		return false
	}
	id, ok := d[0].Value.(bson.Binary)
	return ok && id.Subtype == 4 && len(id.Data) == 16
}

func mongoPolicyCommandOK(doc bson.D) bool {
	if len(doc) < 2 {
		return false
	}
	command := doc[0].Key
	var fields, required string
	switch command {
	case "ping", "buildInfo", "buildinfo":
	case "find":
		fields = "filter projection sort skip limit batchSize singleBatch hint min max returnKey showRecordId tailable awaitData noCursorTimeout allowPartialResults allowDiskUse let readConcern"
	case "aggregate":
		fields, required = "pipeline cursor allowDiskUse hint let readConcern", "pipeline cursor"
	case "count":
		fields = "query limit skip hint readConcern"
	case "distinct":
		fields, required = "key query readConcern", "key"
	case "getMore":
		fields, required = "collection batchSize", "collection"
	case "killCursors":
		fields, required = "cursors", "cursors"
	case "insert":
		fields, required = "documents ordered writeConcern bypassDocumentValidation", "documents"
	case "update":
		fields, required = "updates ordered writeConcern bypassDocumentValidation let", "updates"
	case "delete":
		fields, required = "deletes ordered writeConcern let", "deletes"
	case "findAndModify":
		fields = "query sort remove update new upsert fields arrayFilters bypassDocumentValidation writeConcern let hint"
	case "listDatabases":
		fields = "filter nameOnly authorizedDatabases"
	case "listCollections":
		fields = "filter nameOnly authorizedCollections cursor"
	case "listIndexes":
		fields = "cursor"
	case "endSessions":
	default:
		return false
	}
	if !mongoPolicyFields(doc, command+" $db lsid maxTimeMS $readPreference "+fields) {
		return false
	}
	if !mongoPolicyDB(mongoPolicyGet(doc, "$db"), mongoPolicyHas("ping buildInfo buildinfo listDatabases endSessions", command)) {
		return false
	}
	for _, key := range strings.Fields(required) {
		if mongoPolicyGet(doc, key) == nil {
			return false
		}
	}
	switch command {
	case "ping", "buildInfo", "buildinfo", "listDatabases", "listCollections":
		if !mongoPolicyRange(doc[0].Value, 1, 1) {
			return false
		}
	case "endSessions":
		a, ok := doc[0].Value.(bson.A)
		if !ok || len(a) == 0 {
			return false
		}
		for _, v := range a {
			if !mongoPolicySession(v) {
				return false
			}
		}
	case "getMore":
		if _, ok := mongoPolicyInt(doc[0].Value); !ok || !mongoPolicyCollection(mongoPolicyGet(doc, "collection")) {
			return false
		}
	default:
		if !mongoPolicyCollection(doc[0].Value) {
			return false
		}
	}
	for _, e := range doc[1:] {
		switch e.Key {
		case "$db":
		case "lsid":
			if !mongoPolicySession(e.Value) {
				return false
			}
		case "$readPreference":
			d, ok := e.Value.(bson.D)
			if !ok || len(d) != 1 || d[0].Key != "mode" {
				return false
			}
			mode, ok := d[0].Value.(string)
			if !ok || !mongoPolicyHas("primary primaryPreferred secondary secondaryPreferred nearest", mode) {
				return false
			}
		case "maxTimeMS":
			if !mongoPolicyRange(e.Value, 0, math.MaxInt32) {
				return false
			}
		case "batchSize", "skip":
			if !mongoPolicyRange(e.Value, 0, math.MaxInt64) {
				return false
			}
		case "limit":
			if !mongoPolicyRange(e.Value, 0, math.MaxInt64) {
				return false
			}
		case "singleBatch", "returnKey", "showRecordId", "tailable", "awaitData", "noCursorTimeout", "allowPartialResults", "allowDiskUse", "ordered", "bypassDocumentValidation", "new", "upsert", "remove", "nameOnly", "authorizedDatabases", "authorizedCollections":
			if _, ok := e.Value.(bool); !ok {
				return false
			}
		case "filter", "query":
			if command == "listCollections" || command == "listDatabases" {
				fields := "name"
				if command == "listCollections" {
					fields += " type"
				}
				if !mongoPolicyMetadataFilter(e.Value, fields, 0) {
					return false
				}
			} else if !mongoPolicyMatch(e.Value, 0) {
				return false
			}
		case "projection", "fields":
			if !mongoPolicyProjection(e.Value, 0, true) {
				return false
			}
		case "sort":
			if !mongoPolicySort(e.Value) {
				return false
			}
		case "hint":
			if !mongoPolicyHint(e.Value) {
				return false
			}
		case "min", "max":
			if !mongoPolicyFieldDoc(e.Value) {
				return false
			}
		case "let":
			if !mongoPolicyBindings(e.Value, 0) {
				return false
			}
		case "readConcern":
			d, ok := e.Value.(bson.D)
			if !ok || !mongoPolicyFields(d, "level afterClusterTime") {
				return false
			}
			for _, f := range d {
				if f.Key == "level" {
					s, ok := f.Value.(string)
					if !ok || !mongoPolicyHas("local majority available linearizable", s) {
						return false
					}
				} else if _, ok := f.Value.(bson.Timestamp); !ok {
					return false
				}
			}
		case "writeConcern":
			if !mongoPolicyWriteConcern(e.Value) {
				return false
			}
		case "cursor":
			d, ok := e.Value.(bson.D)
			if !ok || !mongoPolicyFields(d, "batchSize") {
				return false
			}
			if len(d) > 0 && !mongoPolicyRange(d[0].Value, 0, math.MaxInt32) {
				return false
			}
		case "pipeline":
			if !mongoPolicyPipeline(e.Value, 0, false) {
				return false
			}
		case "documents":
			a, ok := e.Value.(bson.A)
			if !ok || len(a) == 0 {
				return false
			}
			for _, v := range a {
				if _, ok := v.(bson.D); !ok {
					return false
				}
			}
		case "updates", "deletes":
			if !mongoPolicyWrites(e.Value, e.Key == "updates") {
				return false
			}
		case "update":
			if !mongoPolicyUpdate(e.Value, 0) {
				return false
			}
		case "arrayFilters":
			if !mongoPolicyMatchArray(e.Value, 0) {
				return false
			}
		case "key":
			if !mongoPolicyField(e.Value) {
				return false
			}
		case "collection":
			if !mongoPolicyCollection(e.Value) {
				return false
			}
		case "cursors":
			a, ok := e.Value.(bson.A)
			if !ok {
				return false
			}
			for _, v := range a {
				if _, ok := mongoPolicyInt(v); !ok {
					return false
				}
			}
		default:
			return false
		}
	}
	if command == "findAndModify" {
		remove, _ := mongoPolicyGet(doc, "remove").(bool)
		if remove == (mongoPolicyGet(doc, "update") != nil) {
			return false
		}
	}
	return true
}

// A predicate can disclose private metadata even when its matching reply is
// sanitized. Only boolean combinations over these exact public fields are safe.
func mongoPolicyMetadataFilter(value any, fields string, depth int) bool {
	if depth > 64 {
		return false
	}
	doc, ok := value.(bson.D)
	if !ok || !mongoPolicyFields(doc, fields+" $and $or") {
		return false
	}
	for _, e := range doc {
		if e.Key == "$and" || e.Key == "$or" {
			filters, ok := e.Value.(bson.A)
			if !ok || len(filters) == 0 {
				return false
			}
			for _, filter := range filters {
				if !mongoPolicyMetadataFilter(filter, fields, depth+1) {
					return false
				}
			}
			continue
		}
		operands := bson.A{e.Value}
		if predicate, ok := e.Value.(bson.D); ok {
			if len(predicate) == 0 || !mongoPolicyFields(predicate, "$eq $regex $options $in") {
				return false
			}
			operands = nil
			for _, op := range predicate {
				switch op.Key {
				case "$eq":
					if _, ok := op.Value.(string); !ok {
						return false
					}
				case "$regex":
					operands = append(operands, op.Value)
				case "$options":
					options, ok := op.Value.(string)
					if !ok || strings.Trim(options, "imsxu") != "" || !mongoPolicyPresent(predicate, "$regex") {
						return false
					}
				case "$in":
					values, ok := op.Value.(bson.A)
					if !ok {
						return false
					}
					operands = append(operands, values...)
				}
			}
		}
		for _, operand := range operands {
			switch operand := operand.(type) {
			case string:
			case bson.Regex:
				if strings.Trim(operand.Options, "imsxu") != "" {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

func mongoPolicyWriteConcern(value any) bool {
	d, ok := value.(bson.D)
	if !ok || !mongoPolicyFields(d, "w j wtimeout") {
		return false
	}
	for _, e := range d {
		switch e.Key {
		case "w":
			if e.Value != "majority" && !mongoPolicyRange(e.Value, 1, math.MaxInt32) {
				return false
			}
		case "j":
			if _, ok := e.Value.(bool); !ok {
				return false
			}
		case "wtimeout":
			if !mongoPolicyRange(e.Value, 0, math.MaxInt32) {
				return false
			}
		}
	}
	return true
}

func mongoPolicyFieldDoc(value any) bool {
	d, ok := value.(bson.D)
	if !ok {
		return false
	}
	seen := make(map[string]bool, len(d))
	for _, e := range d {
		if !mongoPolicyField(e.Key) || seen[e.Key] {
			return false
		}
		seen[e.Key] = true
	}
	return true
}

func mongoPolicySort(value any) bool {
	if !mongoPolicyFieldDoc(value) {
		return false
	}
	for _, e := range value.(bson.D) {
		n, ok := mongoPolicyInt(e.Value)
		if !ok || n != 1 && n != -1 {
			return false
		}
	}
	return true
}

func mongoPolicyHint(value any) bool {
	if _, ok := value.(string); ok {
		return mongoPolicyField(value)
	}
	return mongoPolicySort(value)
}

func mongoPolicyMatchArray(value any, depth int) bool {
	a, ok := value.(bson.A)
	if !ok || len(a) == 0 {
		return false
	}
	for _, v := range a {
		if !mongoPolicyMatch(v, depth+1) {
			return false
		}
	}
	return true
}

func mongoPolicyMatch(value any, depth int) bool {
	if depth > 64 {
		return false
	}
	d, ok := value.(bson.D)
	if !ok {
		return false
	}
	seen := make(map[string]bool, len(d))
	for _, e := range d {
		if seen[e.Key] {
			return false
		}
		seen[e.Key] = true
		switch e.Key {
		case "$and", "$or", "$nor":
			if !mongoPolicyMatchArray(e.Value, depth+1) {
				return false
			}
		case "$expr":
			if !mongoPolicyExpression(e.Value, depth+1) {
				return false
			}
		default:
			if !mongoPolicyField(e.Key) || !mongoPolicyCondition(e.Value, depth+1) {
				return false
			}
		}
	}
	return true
}

func mongoPolicyCondition(value any, depth int) bool {
	if depth > 64 {
		return false
	}
	d, ok := value.(bson.D)
	if !ok {
		return mongoPolicyDataOperand(value)
	}
	operators := false
	for _, e := range d {
		if strings.HasPrefix(e.Key, "$") {
			operators = true
		}
	}
	if !operators {
		return true
	} // Embedded-document equality is business data.
	if !mongoPolicyFields(d, "$eq $ne $gt $gte $lt $lte $in $nin $exists $type $regex $options $size $all $elemMatch $not $mod $bitsAllClear $bitsAllSet $bitsAnyClear $bitsAnySet") {
		return false
	}
	for _, e := range d {
		switch e.Key {
		case "$eq", "$ne", "$gt", "$gte", "$lt", "$lte": // Literal operands, not expressions.
		case "$in", "$nin":
			if _, ok := e.Value.(bson.A); !ok {
				return false
			}
		case "$exists":
			if _, ok := e.Value.(bool); !ok {
				return false
			}
		case "$size":
			if !mongoPolicyRange(e.Value, 0, math.MaxInt32) {
				return false
			}
		case "$type":
			if a, ok := e.Value.(bson.A); ok {
				for _, v := range a {
					if !mongoPolicyBSONType(v) {
						return false
					}
				}
			} else if !mongoPolicyBSONType(e.Value) {
				return false
			}
		case "$regex":
			switch e.Value.(type) {
			case string, bson.Regex:
			default:
				return false
			}
		case "$options":
			s, ok := e.Value.(string)
			if !ok || mongoPolicyGet(d, "$regex") == nil || strings.Trim(s, "imsxu") != "" {
				return false
			}
		case "$not":
			if _, ok := e.Value.(bson.Regex); ok {
				continue
			}
			x, ok := e.Value.(bson.D)
			if !ok || len(x) == 0 || !strings.HasPrefix(x[0].Key, "$") || !mongoPolicyCondition(x, depth+1) {
				return false
			}
		case "$elemMatch":
			if !mongoPolicyElementMatch(e.Value, depth+1) {
				return false
			}
		case "$all":
			a, ok := e.Value.(bson.A)
			if !ok {
				return false
			}
			for _, v := range a {
				if x, ok := v.(bson.D); ok {
					for _, f := range x {
						if strings.HasPrefix(f.Key, "$") && (len(x) != 1 || f.Key != "$elemMatch" || !mongoPolicyElementMatch(f.Value, depth+1)) {
							return false
						}
					}
				} else if !mongoPolicyDataOperand(v) {
					return false
				}
			}
		case "$mod":
			a, ok := e.Value.(bson.A)
			if !ok || len(a) != 2 {
				return false
			}
			for _, v := range a {
				if !mongoPolicyNumber(v) {
					return false
				}
			}
		default: // Bit tests accept a numeric mask, binary mask, or bit positions.
			if a, ok := e.Value.(bson.A); ok {
				for _, v := range a {
					if !mongoPolicyRange(v, 0, math.MaxInt32) {
						return false
					}
				}
			} else if _, ok := e.Value.(bson.Binary); !ok && !mongoPolicyRange(e.Value, 0, math.MaxInt64) {
				return false
			}
		}
	}
	return true
}

// Arrays here are literal equality operands. Opaque document encoders are not
// accepted where a document could instead mean query or modifier syntax.
func mongoPolicyDataOperand(value any) bool {
	switch value.(type) {
	case nil, string, bool, int, int32, int64, float64, bson.A, bson.Decimal128, bson.Binary, bson.Timestamp, bson.ObjectID, bson.DateTime, time.Time, bson.Null, bson.Undefined, bson.MinKey, bson.MaxKey, bson.Regex, bson.JavaScript, bson.CodeWithScope, bson.Symbol, bson.DBPointer:
		return true
	}
	return false
}

func mongoPolicyElementMatch(value any, depth int) bool {
	d, ok := value.(bson.D)
	if !ok {
		return false
	}
	if len(d) > 0 && strings.HasPrefix(d[0].Key, "$") && !mongoPolicyHas("$and $or $nor $expr", d[0].Key) {
		return mongoPolicyCondition(d, depth+1)
	}
	return mongoPolicyMatch(d, depth+1)
}

func mongoPolicyNumber(value any) bool {
	switch n := value.(type) {
	case int, int32, int64, bson.Decimal128:
		return true
	case float64:
		return !math.IsNaN(n) && !math.IsInf(n, 0)
	}
	return false
}

func mongoPolicyBSONType(value any) bool {
	if s, ok := value.(string); ok {
		return mongoPolicyHas("double string object array binData undefined objectId bool date null regex dbPointer javascript symbol javascriptWithScope int timestamp long decimal minKey maxKey number", s)
	}
	n, ok := mongoPolicyInt(value)
	return ok && (n >= 1 && n <= 19 || n == -1 || n == 127)
}

func mongoPolicyBindings(value any, depth int) bool {
	if !mongoPolicyFieldDoc(value) {
		return false
	}
	for _, e := range value.(bson.D) {
		if !mongoPolicyVariable(e.Key) || !mongoPolicyExpression(e.Value, depth+1) {
			return false
		}
	}
	return true
}

func mongoPolicyVariable(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, c := range name {
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func mongoPolicyExpression(value any, depth int) bool {
	if depth > 64 {
		return false
	}
	switch v := value.(type) {
	case string:
		if !strings.HasPrefix(v, "$$") {
			return true
		}
		name := strings.SplitN(v[2:], ".", 2)[0]
		return mongoPolicyHas("ROOT CURRENT NOW CLUSTER_TIME REMOVE", name) || mongoPolicyVariable(name)
	case bson.A:
		for _, item := range v {
			if !mongoPolicyExpression(item, depth+1) {
				return false
			}
		}
	case bson.D:
		seen := make(map[string]bool, len(v))
		for _, e := range v {
			if seen[e.Key] {
				return false
			}
			seen[e.Key] = true
			if strings.HasPrefix(e.Key, "$") {
				if len(v) != 1 {
					return false
				}
				if e.Key == "$literal" {
					return true
				}
				return mongoPolicyOperator(e.Key, e.Value, depth+1)
			}
			if !mongoPolicyExpression(e.Value, depth+1) {
				return false
			}
		}
	case nil, bool, int, int32, int64, float64, bson.Decimal128, bson.Binary, bson.Timestamp, bson.ObjectID, bson.DateTime, time.Time, bson.Null, bson.Undefined, bson.MinKey, bson.MaxKey, bson.Regex:
	default:
		return false
	}
	return true
}

func mongoPolicyOperator(operator string, value any, depth int) bool {
	if depth > 64 {
		return false
	}
	min, max := -1, -1
	var fields, required string
	switch operator {
	case "$abs", "$ceil", "$exp", "$floor", "$ln", "$log10", "$sqrt", "$isArray", "$arrayToObject", "$objectToArray", "$reverseArray", "$size", "$strLenBytes", "$strLenCP", "$toLower", "$toUpper", "$toBool", "$toDate", "$toDecimal", "$toDouble", "$toInt", "$toLong", "$toObjectId", "$toString", "$isNumber", "$type", "$first", "$last":
		if a, ok := value.(bson.A); ok && len(a) != 1 {
			return false
		}
	case "$sum", "$avg", "$min", "$max", "$push", "$addToSet", "$stdDevPop", "$stdDevSamp", "$mergeObjects":
	case "$add", "$multiply", "$concat", "$concatArrays":
		min, max = 1, math.MaxInt32
	case "$and", "$or":
		min, max = 0, math.MaxInt32
	case "$divide", "$log", "$mod", "$pow", "$subtract", "$cmp", "$eq", "$gt", "$gte", "$lt", "$lte", "$ne", "$arrayElemAt", "$in", "$setDifference", "$setIsSubset", "$split", "$strcasecmp":
		min, max = 2, 2
	case "$not", "$allElementsTrue", "$anyElementTrue":
		min, max = 1, 1
	case "$round", "$trunc":
		if _, ok := value.(bson.A); ok {
			min, max = 1, 2
		}
	case "$ifNull", "$setEquals", "$setIntersection", "$setUnion":
		min, max = 2, math.MaxInt32
	case "$indexOfArray", "$indexOfBytes", "$indexOfCP":
		min, max = 2, 4
	case "$slice", "$range":
		min, max = 2, 3
	case "$substr", "$substrBytes", "$substrCP":
		min, max = 3, 3
	case "$cond":
		if _, ok := value.(bson.A); ok {
			min, max = 3, 3
		} else {
			fields, required = "if then else", "if then else"
		}
	case "$filter":
		fields, required = "input as cond limit", "input cond"
	case "$map":
		fields, required = "input as in", "input in"
	case "$reduce":
		fields, required = "input initialValue in", "input initialValue in"
	case "$let":
		fields, required = "vars in", "vars in"
	case "$switch":
		fields, required = "branches default", "branches"
	case "$sortArray":
		fields, required = "input sortBy", "input sortBy"
	case "$zip":
		fields, required = "inputs useLongestLength defaults", "inputs"
	case "$trim", "$ltrim", "$rtrim":
		fields, required = "input chars", "input"
	case "$replaceOne", "$replaceAll":
		fields, required = "input find replacement", "input find replacement"
	case "$regexFind", "$regexFindAll", "$regexMatch":
		fields, required = "input regex options", "input regex"
	case "$convert":
		fields, required = "input to onError onNull", "input to"
	case "$dateAdd", "$dateSubtract":
		fields, required = "startDate unit amount timezone", "startDate unit amount"
	case "$dateDiff":
		fields, required = "startDate endDate unit timezone startOfWeek", "startDate endDate unit"
	case "$dateTrunc":
		fields, required = "date unit binSize timezone startOfWeek", "date unit"
	case "$dateFromParts":
		fields = "year month day hour minute second millisecond timezone isoWeekYear isoWeek isoDayOfWeek"
	case "$dateFromString":
		fields, required = "dateString format timezone onError onNull", "dateString"
	case "$dateToParts":
		fields, required = "date timezone iso8601", "date"
	case "$dateToString":
		fields, required = "date format timezone onNull", "date"
	case "$dayOfMonth", "$dayOfWeek", "$dayOfYear", "$hour", "$isoDayOfWeek", "$isoWeek", "$isoWeekYear", "$millisecond", "$minute", "$month", "$second", "$week", "$year":
		if d, ok := value.(bson.D); ok && mongoPolicyPresent(d, "date") {
			fields, required = "date timezone", "date"
		}
	case "$firstN", "$lastN", "$minN", "$maxN":
		fields, required = "input n", "input n"
	case "$median":
		fields, required = "input method", "input method"
	case "$percentile":
		fields, required = "input p method", "input p method"
	case "$count":
		d, ok := value.(bson.D)
		return ok && len(d) == 0
	default:
		return false
	}
	if min >= 0 {
		a, ok := value.(bson.A)
		if !ok || len(a) < min || len(a) > max {
			return false
		}
	}
	if fields != "" {
		d, ok := value.(bson.D)
		if !ok || !mongoPolicyFields(d, fields) {
			return false
		}
		for _, key := range strings.Fields(required) {
			if !mongoPolicyPresent(d, key) {
				return false
			}
		}
		for _, e := range d {
			switch {
			case e.Key == "as":
				name, ok := e.Value.(string)
				if !ok || !mongoPolicyVariable(name) {
					return false
				}
			case operator == "$let" && e.Key == "vars":
				if !mongoPolicyBindings(e.Value, depth+1) {
					return false
				}
			case operator == "$switch" && e.Key == "branches":
				a, ok := e.Value.(bson.A)
				if !ok || len(a) == 0 {
					return false
				}
				for _, v := range a {
					x, ok := v.(bson.D)
					if !ok || len(x) != 2 || !mongoPolicyFields(x, "case then") || !mongoPolicyExpression(x, depth+1) {
						return false
					}
				}
			case operator == "$sortArray" && e.Key == "sortBy":
				if n, ok := mongoPolicyInt(e.Value); !ok || n != -1 && n != 1 {
					if !mongoPolicySort(e.Value) {
						return false
					}
				}
			case operator == "$zip" && e.Key == "useLongestLength":
				if _, ok := e.Value.(bool); !ok {
					return false
				}
			case e.Key == "method":
				method, ok := e.Value.(string)
				if !ok || method != "approximate" {
					return false
				}
			default:
				if !mongoPolicyExpression(e.Value, depth+1) {
					return false
				}
			}
		}
		return true
	}
	return mongoPolicyExpression(value, depth+1)
}

func mongoPolicyProjection(value any, depth int, find bool) bool {
	if !mongoPolicyFieldDoc(value) {
		return false
	}
	for _, e := range value.(bson.D) {
		if d, ok := e.Value.(bson.D); find && ok && len(d) == 1 && d[0].Key == "$elemMatch" {
			if !mongoPolicyElementMatch(d[0].Value, depth+1) {
				return false
			}
		} else if find && ok && len(d) == 1 && d[0].Key == "$slice" {
			if a, ok := d[0].Value.(bson.A); ok {
				if len(a) != 2 {
					return false
				}
				if _, ok := mongoPolicyInt(a[0]); !ok || !mongoPolicyRange(a[1], 1, math.MaxInt32) {
					return false
				}
			} else if _, ok := mongoPolicyInt(d[0].Value); !ok {
				return false
			}
		} else if !mongoPolicyExpression(e.Value, depth+1) {
			return false
		}
	}
	return true
}

func mongoPolicyPipeline(value any, depth int, update bool) bool {
	if depth > 64 {
		return false
	}
	a, ok := value.(bson.A)
	if !ok {
		return false
	}
	for _, item := range a {
		d, ok := item.(bson.D)
		if !ok || len(d) != 1 {
			return false
		}
		e := d[0]
		if update && !mongoPolicyHas("$set $addFields $project $unset $replaceRoot $replaceWith", e.Key) {
			return false
		}
		switch e.Key {
		case "$match":
			if !mongoPolicyMatch(e.Value, depth+1) {
				return false
			}
		case "$project", "$set", "$addFields":
			if !mongoPolicyProjection(e.Value, depth+1, false) {
				return false
			}
		case "$group":
			if !mongoPolicyFieldDoc(e.Value) || !mongoPolicyPresent(e.Value.(bson.D), "_id") {
				return false
			}
			for _, f := range e.Value.(bson.D) {
				if f.Key != "_id" {
					x, ok := f.Value.(bson.D)
					if !ok || len(x) != 1 || !mongoPolicyHas("$sum $avg $min $max $first $last $push $addToSet $stdDevPop $stdDevSamp $firstN $lastN $maxN $minN $count $median $percentile $mergeObjects", x[0].Key) {
						return false
					}
				}
				if !mongoPolicyExpression(f.Value, depth+1) {
					return false
				}
			}
		case "$sort":
			if !mongoPolicySort(e.Value) {
				return false
			}
		case "$skip":
			if !mongoPolicyRange(e.Value, 0, math.MaxInt64) {
				return false
			}
		case "$limit":
			if !mongoPolicyRange(e.Value, 1, math.MaxInt64) {
				return false
			}
		case "$count":
			if !mongoPolicyField(e.Value) {
				return false
			}
		case "$unset":
			if a, ok := e.Value.(bson.A); ok {
				for _, v := range a {
					if !mongoPolicyField(v) {
						return false
					}
				}
			} else if !mongoPolicyField(e.Value) {
				return false
			}
		case "$unwind":
			if path, ok := e.Value.(string); ok {
				if !mongoPolicyPath(path) {
					return false
				}
				continue
			}
			x, ok := e.Value.(bson.D)
			if !ok || !mongoPolicyFields(x, "path includeArrayIndex preserveNullAndEmptyArrays") || !mongoPolicyPath(mongoPolicyGet(x, "path")) {
				return false
			}
			for _, f := range x {
				if f.Key == "includeArrayIndex" && !mongoPolicyField(f.Value) {
					return false
				}
				if f.Key == "preserveNullAndEmptyArrays" {
					if _, ok := f.Value.(bool); !ok {
						return false
					}
				}
			}
		case "$lookup":
			x, ok := e.Value.(bson.D)
			if !ok || !mongoPolicyFields(x, "from as localField foreignField let pipeline") || !mongoPolicyCollection(mongoPolicyGet(x, "from")) || !mongoPolicyField(mongoPolicyGet(x, "as")) {
				return false
			}
			local, foreign := mongoPolicyGet(x, "localField"), mongoPolicyGet(x, "foreignField")
			if (local != nil || foreign != nil) && (!mongoPolicyField(local) || !mongoPolicyField(foreign)) {
				return false
			}
			pipeline := mongoPolicyGet(x, "pipeline")
			if pipeline == nil && local == nil {
				return false
			}
			if mongoPolicyPresent(x, "pipeline") && !mongoPolicyPipeline(pipeline, depth+1, false) {
				return false
			}
			if mongoPolicyPresent(x, "let") && !mongoPolicyBindings(mongoPolicyGet(x, "let"), depth+1) {
				return false
			}
		case "$unionWith":
			if _, ok := e.Value.(string); ok {
				if !mongoPolicyCollection(e.Value) {
					return false
				}
				continue
			}
			x, ok := e.Value.(bson.D)
			if !ok || !mongoPolicyFields(x, "coll pipeline") || !mongoPolicyCollection(mongoPolicyGet(x, "coll")) {
				return false
			}
			if mongoPolicyPresent(x, "pipeline") && !mongoPolicyPipeline(mongoPolicyGet(x, "pipeline"), depth+1, false) {
				return false
			}
		case "$facet":
			if !mongoPolicyFieldDoc(e.Value) {
				return false
			}
			for _, f := range e.Value.(bson.D) {
				if !mongoPolicyPipeline(f.Value, depth+1, false) {
					return false
				}
			}
		case "$replaceRoot":
			x, ok := e.Value.(bson.D)
			if !ok || len(x) != 1 || x[0].Key != "newRoot" || !mongoPolicyExpression(x[0].Value, depth+1) {
				return false
			}
		case "$replaceWith", "$sortByCount":
			if !mongoPolicyExpression(e.Value, depth+1) {
				return false
			}
		case "$sample":
			x, ok := e.Value.(bson.D)
			if !ok || len(x) != 1 || x[0].Key != "size" || !mongoPolicyRange(x[0].Value, 0, math.MaxInt64) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func mongoPolicyPath(value any) bool {
	s, ok := value.(string)
	return ok && strings.HasPrefix(s, "$") && mongoPolicyField(s[1:])
}

func mongoPolicyWrites(value any, update bool) bool {
	a, ok := value.(bson.A)
	if !ok || len(a) == 0 {
		return false
	}
	for _, item := range a {
		d, ok := item.(bson.D)
		if !ok {
			return false
		}
		fields := "q limit hint"
		if update {
			fields = "q u upsert multi arrayFilters hint"
		}
		if !mongoPolicyFields(d, fields) || !mongoPolicyMatch(mongoPolicyGet(d, "q"), 0) {
			return false
		}
		if update {
			if !mongoPolicyUpdate(mongoPolicyGet(d, "u"), 0) {
				return false
			}
		} else if !mongoPolicyRange(mongoPolicyGet(d, "limit"), 0, 1) {
			return false
		}
		for _, e := range d {
			switch e.Key {
			case "upsert", "multi":
				if _, ok := e.Value.(bool); !ok {
					return false
				}
			case "hint":
				if !mongoPolicyHint(e.Value) {
					return false
				}
			case "arrayFilters":
				if !mongoPolicyMatchArray(e.Value, 0) {
					return false
				}
			}
		}
	}
	return true
}

func mongoPolicyUpdate(value any, depth int) bool {
	if _, ok := value.(bson.A); ok {
		return mongoPolicyPipeline(value, depth+1, true)
	}
	d, ok := value.(bson.D)
	if !ok {
		return false
	}
	modifiers := false
	for _, e := range d {
		if strings.HasPrefix(e.Key, "$") {
			modifiers = true
		}
	}
	if !modifiers {
		return true
	} // Replacement documents are not expression contexts.
	if !mongoPolicyFields(d, "$set $unset $inc $mul $min $max $rename $setOnInsert $currentDate $addToSet $pop $pull $push $pullAll $bit") {
		return false
	}
	for _, e := range d {
		if !mongoPolicyFieldDoc(e.Value) {
			return false
		}
		for _, f := range e.Value.(bson.D) {
			switch e.Key {
			case "$set", "$unset", "$min", "$max", "$setOnInsert":
			case "$inc", "$mul":
				if !mongoPolicyNumber(f.Value) {
					return false
				}
			case "$rename":
				if !mongoPolicyField(f.Value) {
					return false
				}
			case "$pop":
				n, ok := mongoPolicyInt(f.Value)
				if !ok || n != 1 && n != -1 {
					return false
				}
			case "$pullAll":
				if _, ok := f.Value.(bson.A); !ok {
					return false
				}
			case "$pull":
				if x, ok := f.Value.(bson.D); ok {
					if !mongoPolicyElementMatch(x, depth+1) {
						return false
					}
				} else if !mongoPolicyDataOperand(f.Value) {
					return false
				}
			case "$currentDate":
				if b, ok := f.Value.(bool); ok {
					if !b {
						return false
					}
					continue
				}
				x, ok := f.Value.(bson.D)
				if !ok || len(x) != 1 || x[0].Key != "$type" {
					return false
				}
				s, ok := x[0].Value.(string)
				if !ok || !mongoPolicyHas("date timestamp", s) {
					return false
				}
			case "$bit":
				x, ok := f.Value.(bson.D)
				if !ok || !mongoPolicyFields(x, "and or xor") {
					return false
				}
				for _, bit := range x {
					if _, ok := mongoPolicyInt(bit.Value); !ok {
						return false
					}
				}
			case "$push", "$addToSet":
				x, ok := f.Value.(bson.D)
				if !ok {
					if !mongoPolicyDataOperand(f.Value) {
						return false
					}
					continue
				}
				control := false
				for _, part := range x {
					if strings.HasPrefix(part.Key, "$") {
						control = true
					}
				}
				if !control {
					continue
				}
				fields := "$each"
				if e.Key == "$push" {
					fields += " $slice $position $sort"
				}
				if !mongoPolicyFields(x, fields) {
					return false
				}
				if _, ok := mongoPolicyGet(x, "$each").(bson.A); !ok {
					return false
				}
				for _, part := range x {
					switch part.Key {
					case "$slice", "$position":
						if _, ok := mongoPolicyInt(part.Value); !ok {
							return false
						}
					case "$sort":
						if n, ok := mongoPolicyInt(part.Value); !ok || n != -1 && n != 1 {
							if !mongoPolicySort(part.Value) {
								return false
							}
						}
					}
				}
			}
		}
	}
	return true
}

// Call only after mongoCheckCommand succeeds. Duplicates are collapsed in first-use order.
func mongoCommandNamespaces(doc bson.D) []mongoNamespace {
	if mongoCheckCommand(doc) != nil {
		return nil
	}
	db := mongoPolicyGet(doc, "$db").(string)
	var result []mongoNamespace
	add := func(collection string) {
		ns := mongoNamespace{Database: db, Collection: collection}
		for _, existing := range result {
			if existing == ns {
				return
			}
		}
		result = append(result, ns)
	}
	switch doc[0].Key {
	case "find", "aggregate", "count", "distinct", "killCursors", "insert", "update", "delete", "findAndModify", "listIndexes":
		add(doc[0].Value.(string))
	case "getMore":
		add(mongoPolicyGet(doc, "collection").(string))
	}
	var walk func(bson.A)
	walk = func(pipeline bson.A) {
		for _, v := range pipeline {
			e := v.(bson.D)[0]
			switch e.Key {
			case "$lookup":
				d := e.Value.(bson.D)
				add(mongoPolicyGet(d, "from").(string))
				if p, ok := mongoPolicyGet(d, "pipeline").(bson.A); ok {
					walk(p)
				}
			case "$unionWith":
				if s, ok := e.Value.(string); ok {
					add(s)
				} else {
					d := e.Value.(bson.D)
					add(mongoPolicyGet(d, "coll").(string))
					if p, ok := mongoPolicyGet(d, "pipeline").(bson.A); ok {
						walk(p)
					}
				}
			case "$facet":
				for _, f := range e.Value.(bson.D) {
					walk(f.Value.(bson.A))
				}
			}
		}
	}
	if doc[0].Key == "aggregate" {
		walk(mongoPolicyGet(doc, "pipeline").(bson.A))
	}
	return result
}

// For getMore on metadata cursors, the caller MUST supply the original list command.
// Cursor IDs/namespaces must also be bound and checked by the tunnel before forwarding.
func mongoPublicReply(command string, reply bson.D) bson.D {
	ok := float64(0)
	if mongoPolicyRange(mongoPolicyGet(reply, "ok"), 1, 1) {
		ok = 1
	}
	result := bson.D{{Key: "ok", Value: ok}}
	if ok == 0 {
		result = append(result, mongoPolicyError(reply, false)...)
	} else {
		switch command {
		case "find", "aggregate", "getMore", "listCollections", "listIndexes":
			if cursor, valid := mongoPolicyGet(reply, "cursor").(bson.D); valid {
				clean := bson.D{}
				if id := mongoPolicyGet(cursor, "id"); id != nil {
					if _, valid := mongoPolicyInt(id); valid {
						clean = append(clean, bson.E{Key: "id", Value: id})
					}
				}
				if ns, valid := mongoPolicyGet(cursor, "ns").(string); valid {
					clean = append(clean, bson.E{Key: "ns", Value: ns})
				}
				for _, key := range []string{"firstBatch", "nextBatch"} {
					if batch, valid := mongoPolicyGet(cursor, key).(bson.A); valid {
						if command == "listCollections" || command == "listIndexes" {
							batch = mongoPolicyMetadataBatch(command, batch)
						}
						clean = append(clean, bson.E{Key: key, Value: batch})
					}
				}
				result = append(result, bson.E{Key: "cursor", Value: clean})
			}
		case "findAndModify":
			if value := mongoPolicyGet(reply, "value"); value != nil {
				if _, valid := value.(bson.D); valid {
					result = append(result, bson.E{Key: "value", Value: value})
				}
			} else {
				result = append(result, bson.E{Key: "value", Value: nil})
			}
			if last, valid := mongoPolicyGet(reply, "lastErrorObject").(bson.D); valid {
				clean := mongoPolicyCounts(last, "n")
				if updated, valid := mongoPolicyGet(last, "updatedExisting").(bool); valid {
					clean = append(clean, bson.E{Key: "updatedExisting", Value: updated})
				}
				if id := mongoPolicyGet(last, "upserted"); id != nil {
					clean = append(clean, bson.E{Key: "upserted", Value: id})
				}
				result = append(result, bson.E{Key: "lastErrorObject", Value: clean})
			}
		case "distinct":
			if values, valid := mongoPolicyGet(reply, "values").(bson.A); valid {
				result = append(result, bson.E{Key: "values", Value: values})
			}
		case "count", "insert", "update", "delete":
			result = append(result, mongoPolicyCounts(reply, "n nModified")...)
			if command == "update" {
				if upserted, valid := mongoPolicyGet(reply, "upserted").(bson.A); valid {
					clean := bson.A{}
					for _, item := range upserted {
						if d, valid := item.(bson.D); valid {
							index := mongoPolicyGet(d, "index")
							if !mongoPolicyRange(index, 0, math.MaxInt32) {
								continue
							}
							clean = append(clean, bson.D{{Key: "index", Value: index}, {Key: "_id", Value: mongoPolicyGet(d, "_id")}})
						}
					}
					result = append(result, bson.E{Key: "upserted", Value: clean})
				}
			}
		case "killCursors":
			for _, key := range []string{"cursorsKilled", "cursorsNotFound", "cursorsAlive", "cursorsUnknown"} {
				if ids, valid := mongoPolicyGet(reply, key).(bson.A); valid {
					clean := bson.A{}
					for _, id := range ids {
						if _, valid := mongoPolicyInt(id); valid {
							clean = append(clean, id)
						}
					}
					result = append(result, bson.E{Key: key, Value: clean})
				}
			}
		case "listDatabases":
			if dbs, valid := mongoPolicyGet(reply, "databases").(bson.A); valid {
				clean := bson.A{}
				for _, item := range dbs {
					if d, valid := item.(bson.D); valid {
						name := mongoPolicyGet(d, "name")
						if !mongoPolicyDB(name, false) {
							continue
						}
						x := bson.D{{Key: "name", Value: name}}
						x = append(x, mongoPolicyCounts(d, "sizeOnDisk")...)
						if empty, valid := mongoPolicyGet(d, "empty").(bool); valid {
							x = append(x, bson.E{Key: "empty", Value: empty})
						}
						clean = append(clean, x)
					}
				}
				result = append(result, bson.E{Key: "databases", Value: clean})
			}
		case "buildInfo", "buildinfo":
			if version, valid := mongoPolicyGet(reply, "version").(string); valid && len(version) <= 32 && version != "" && strings.Trim(version, "0123456789.") == "" {
				result = append(result, bson.E{Key: "version", Value: version})
			}
			if versions, valid := mongoPolicyGet(reply, "versionArray").(bson.A); valid && len(versions) == 4 {
				valid = true
				for _, v := range versions {
					valid = valid && mongoPolicyRange(v, 0, math.MaxInt32)
				}
				if valid {
					result = append(result, bson.E{Key: "versionArray", Value: versions})
				}
			}
		}
	}
	if errs, valid := mongoPolicyGet(reply, "writeErrors").(bson.A); valid {
		clean := bson.A{}
		for _, item := range errs {
			if d, valid := item.(bson.D); valid {
				clean = append(clean, mongoPolicyError(d, true))
			}
		}
		result = append(result, bson.E{Key: "writeErrors", Value: clean})
	}
	if err, valid := mongoPolicyGet(reply, "writeConcernError").(bson.D); valid {
		result = append(result, bson.E{Key: "writeConcernError", Value: mongoPolicyError(err, false)})
	}
	if labels, valid := mongoPolicyGet(reply, "errorLabels").(bson.A); valid {
		clean := bson.A{}
		for _, value := range labels {
			if s, valid := value.(string); valid && mongoPolicyHas("RetryableWriteError NoWritesPerformed TransientTransactionError UnknownTransactionCommitResult ResumableChangeStreamError NonResumableChangeStreamError", s) {
				clean = append(clean, s)
			}
		}
		if len(clean) > 0 {
			result = append(result, bson.E{Key: "errorLabels", Value: clean})
		}
	}
	if ts, valid := mongoPolicyGet(reply, "operationTime").(bson.Timestamp); valid {
		result = append(result, bson.E{Key: "operationTime", Value: ts})
	}
	return result
}

func mongoPolicyCounts(doc bson.D, keys string) bson.D {
	result := bson.D{}
	for _, key := range strings.Fields(keys) {
		if value := mongoPolicyGet(doc, key); mongoPolicyRange(value, 0, math.MaxInt64) {
			result = append(result, bson.E{Key: key, Value: value})
		}
	}
	return result
}

func mongoPolicyError(doc bson.D, index bool) bson.D {
	code := mongoPolicyGet(doc, "code")
	if !mongoPolicyRange(code, 0, math.MaxInt32) {
		code = int32(8)
	}
	name := "CommandFailed"
	n, _ := mongoPolicyInt(code)
	switch n {
	case 13:
		name = "Unauthorized"
	case 18:
		name = "AuthenticationFailed"
	case 50:
		name = "MaxTimeMSExpired"
	case 11000:
		name = "DuplicateKey"
	}
	result := bson.D{{Key: "code", Value: code}, {Key: "codeName", Value: name}, {Key: "errmsg", Value: "MongoDB operation failed"}}
	if index {
		result = append(result, mongoPolicyCounts(doc, "index")...)
	}
	return result
}

func mongoPolicyMetadataBatch(command string, batch bson.A) bson.A {
	result := bson.A{}
	for _, item := range batch {
		d, valid := item.(bson.D)
		if !valid {
			continue
		}
		name := mongoPolicyGet(d, "name")
		if command == "listCollections" {
			typ, valid := mongoPolicyGet(d, "type").(string)
			if !valid || typ != "collection" || !mongoPolicyCollection(name) {
				continue
			}
			result = append(result, bson.D{{Key: "name", Value: name}, {Key: "type", Value: "collection"}})
		} else {
			if !mongoPolicyField(name) {
				continue
			}
			key, valid := mongoPolicyGet(d, "key").(bson.D)
			if !valid || !mongoPolicyFieldDoc(key) {
				continue
			}
			valid = true
			for _, part := range key {
				if n, numeric := mongoPolicyInt(part.Value); numeric && (n == 1 || n == -1) {
					continue
				}
				s, text := part.Value.(string)
				if !text || !mongoPolicyHas("text hashed 2d 2dsphere", s) {
					valid = false
				}
			}
			if !valid {
				continue
			}
			x := mongoPolicyCounts(d, "v")
			x = append(x, bson.E{Key: "key", Value: key}, bson.E{Key: "name", Value: name})
			for _, field := range []string{"unique", "sparse", "hidden"} {
				if b, valid := mongoPolicyGet(d, field).(bool); valid {
					x = append(x, bson.E{Key: field, Value: b})
				}
			}
			x = append(x, mongoPolicyCounts(d, "expireAfterSeconds")...)
			result = append(result, x)
		}
	}
	return result
}
