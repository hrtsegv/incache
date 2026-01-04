package main

import (
	"fmt"

	"github.com/knbr13/incache"
)

func main() {
	c := incache.NewLFU[string, int](4)
	c.Set("one", 1)
	c.Set("two", 2)
	c.Set("three", 3)

	c.Get("one")
	c.Get("two")
	c.Get("three")
	c.Inspect()

	println()
	c.Set("four", 4)
	print("===================\n")
	c.Inspect()
	print("===================\n")
	c.Set("five", 5)

	fmt.Println(c.Keys())
	println()

	c.Inspect()

	c.Set("six", 6)
	print("===================\n")
	c.Inspect()

	c.Get("six")
	c.Get("six")
	c.Get("six")
	c.Get("six")
	c.Get("six")

	c.Set("seven", 7)

	print("===================\n")
	c.Inspect()
}

// func (l *LFUCache[K, V]) Freq() {
// 	for first := l.evictionList.Front(); first != nil; first = first.Next() {
// 		fmt.Println(first.Value.(*lfuItem[K, V]).key, first.Value.(*lfuItem[K, V]).freq)
// 	}
// }
