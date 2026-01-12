package utils

// SliceUtils 切片工具集
// 提供泛型切片操作，消除重复代码

// Unique 去重（保持顺序）
func Unique[T comparable](slice []T) []T {
	seen := make(map[T]bool)
	result := make([]T, 0, len(slice))
	for _, v := range slice {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// Contains 检查切片是否包含元素
func Contains[T comparable](slice []T, item T) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

// ContainsFunc 使用自定义函数检查切片是否包含元素
func ContainsFunc[T any](slice []T, predicate func(T) bool) bool {
	for _, v := range slice {
		if predicate(v) {
			return true
		}
	}
	return false
}

// Filter 过滤切片
func Filter[T any](slice []T, predicate func(T) bool) []T {
	result := make([]T, 0)
	for _, v := range slice {
		if predicate(v) {
			result = append(result, v)
		}
	}
	return result
}

// Map 映射切片
func Map[T any, R any](slice []T, mapper func(T) R) []R {
	result := make([]R, len(slice))
	for i, v := range slice {
		result[i] = mapper(v)
	}
	return result
}

// MapWithIndex 带索引的映射
func MapWithIndex[T any, R any](slice []T, mapper func(int, T) R) []R {
	result := make([]R, len(slice))
	for i, v := range slice {
		result[i] = mapper(i, v)
	}
	return result
}

// Reduce 归约切片
func Reduce[T any, R any](slice []T, initial R, reducer func(R, T) R) R {
	result := initial
	for _, v := range slice {
		result = reducer(result, v)
	}
	return result
}

// Find 查找第一个匹配的元素
func Find[T any](slice []T, predicate func(T) bool) (T, bool) {
	for _, v := range slice {
		if predicate(v) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// FindIndex 查找第一个匹配元素的索引
func FindIndex[T any](slice []T, predicate func(T) bool) int {
	for i, v := range slice {
		if predicate(v) {
			return i
		}
	}
	return -1
}

// All 检查所有元素是否满足条件
func All[T any](slice []T, predicate func(T) bool) bool {
	for _, v := range slice {
		if !predicate(v) {
			return false
		}
	}
	return true
}

// Any 检查是否有任意元素满足条件
func Any[T any](slice []T, predicate func(T) bool) bool {
	for _, v := range slice {
		if predicate(v) {
			return true
		}
	}
	return false
}

// Chunk 将切片分成指定大小的块
func Chunk[T any](slice []T, size int) [][]T {
	if size <= 0 {
		return nil
	}

	var chunks [][]T
	for i := 0; i < len(slice); i += size {
		end := i + size
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}
	return chunks
}

// Flatten 展平二维切片
func Flatten[T any](slices [][]T) []T {
	var result []T
	for _, slice := range slices {
		result = append(result, slice...)
	}
	return result
}

// Reverse 反转切片（返回新切片）
func Reverse[T any](slice []T) []T {
	result := make([]T, len(slice))
	for i, v := range slice {
		result[len(slice)-1-i] = v
	}
	return result
}

// First 获取第一个元素
func First[T any](slice []T) (T, bool) {
	if len(slice) == 0 {
		var zero T
		return zero, false
	}
	return slice[0], true
}

// Last 获取最后一个元素
func Last[T any](slice []T) (T, bool) {
	if len(slice) == 0 {
		var zero T
		return zero, false
	}
	return slice[len(slice)-1], true
}

// Take 获取前n个元素
func Take[T any](slice []T, n int) []T {
	if n <= 0 {
		return nil
	}
	if n >= len(slice) {
		return slice
	}
	return slice[:n]
}

// Skip 跳过前n个元素
func Skip[T any](slice []T, n int) []T {
	if n <= 0 {
		return slice
	}
	if n >= len(slice) {
		return nil
	}
	return slice[n:]
}

// GroupBy 按键分组
func GroupBy[T any, K comparable](slice []T, keyFunc func(T) K) map[K][]T {
	result := make(map[K][]T)
	for _, v := range slice {
		key := keyFunc(v)
		result[key] = append(result[key], v)
	}
	return result
}

// ToMap 将切片转换为map
func ToMap[T any, K comparable, V any](slice []T, keyFunc func(T) K, valueFunc func(T) V) map[K]V {
	result := make(map[K]V)
	for _, v := range slice {
		result[keyFunc(v)] = valueFunc(v)
	}
	return result
}

// Difference 差集（在a中但不在b中）
func Difference[T comparable](a, b []T) []T {
	bSet := make(map[T]bool)
	for _, v := range b {
		bSet[v] = true
	}

	var result []T
	for _, v := range a {
		if !bSet[v] {
			result = append(result, v)
		}
	}
	return result
}

// Intersection 交集
func Intersection[T comparable](a, b []T) []T {
	bSet := make(map[T]bool)
	for _, v := range b {
		bSet[v] = true
	}

	var result []T
	seen := make(map[T]bool)
	for _, v := range a {
		if bSet[v] && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// Union 并集
func Union[T comparable](a, b []T) []T {
	seen := make(map[T]bool)
	var result []T

	for _, v := range a {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	for _, v := range b {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
