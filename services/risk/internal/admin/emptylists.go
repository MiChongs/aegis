package admin

import (
	"encoding"
	"encoding/json"
	"reflect"
)

// emptyLists 返回 v 的一份拷贝，其中 nil 切片换成空切片、nil map 换成空 map，编码后是 [] / {} 而不是 null。
//
// Go 的 nil 切片默认编成 null，控制台对列表直接取 .length / .map：没有关系边的用户、没有命中的场景
// 会整页报错。指针保持原样（null 有意义，如 primaryId）。自带编码的类型（time.Time、json.RawMessage、
// []byte 等）不动。只做拷贝不改原值：返回的可能是进程内共享的数据（如内置模板），原地改会并发写。
func emptyLists(v any) any {
	if v == nil {
		return nil
	}
	return fillEmpty(reflect.ValueOf(v), 0).Interface()
}

var (
	jsonMarshaler = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

func customEncoded(t reflect.Type) bool {
	if t.Implements(jsonMarshaler) || t.Implements(textMarshaler) {
		return true
	}
	if t.Kind() != reflect.Pointer {
		pt := reflect.PointerTo(t)
		return pt.Implements(jsonMarshaler) || pt.Implements(textMarshaler)
	}
	return false
}

// maxFillDepth 防御性上限：响应数据没有环，正常嵌套远小于这个值
const maxFillDepth = 64

func fillEmpty(v reflect.Value, depth int) reflect.Value {
	if !v.IsValid() || depth > maxFillDepth || customEncoded(v.Type()) {
		return v
	}
	t := v.Type()
	switch t.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		p := reflect.New(t.Elem())
		p.Elem().Set(fillEmpty(v.Elem(), depth+1))
		return p
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(t).Elem()
		out.Set(fillEmpty(v.Elem(), depth+1))
		return out
	case reflect.Struct:
		out := reflect.New(t).Elem()
		out.Set(v)
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() && f.Tag.Get("json") != "-" {
				out.Field(i).Set(fillEmpty(v.Field(i), depth+1))
			}
		}
		return out
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 { // []byte 编成 base64 字符串
			return v
		}
		if v.IsNil() {
			return reflect.MakeSlice(t, 0, 0)
		}
		out := reflect.MakeSlice(t, v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(fillEmpty(v.Index(i), depth+1))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.MakeMap(t)
		}
		out := reflect.MakeMapWithSize(t, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), fillEmpty(iter.Value(), depth+1))
		}
		return out
	}
	return v
}
