package klorq

import "fmt"

const maxGatewayPageSize int32 = 1000

func capGatewayPageSize(pageSize int32) int32 {
	if pageSize > maxGatewayPageSize {
		return maxGatewayPageSize
	}
	return pageSize
}

func appendUniquePageItems[T any, K comparable](
	result []T,
	seen map[K]struct{},
	items []T,
	key func(T) K,
	description string,
) ([]T, error) {
	for _, item := range items {
		itemKey := key(item)
		if _, ok := seen[itemKey]; ok {
			return nil, fmt.Errorf("list %s: service returned a duplicate item", description)
		}
		seen[itemKey] = struct{}{}
		result = append(result, item)
	}
	return result, nil
}
