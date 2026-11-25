package utils

import (
	"errors"
	"fmt"
	"strconv"
)

func IntegerValue(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	case string:
		return strconv.ParseInt(v, 10, 64)
	case nil:
		return 0, errors.New("cannot scan NULL enum")
	default:
		return 0, fmt.Errorf("cannot scan enum from %T", value)
	}
}
