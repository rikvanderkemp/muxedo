// SPDX-License-Identifier: MIT
package profile

import (
	"math/rand"
	"time"
)

func RandomName() string {
	adjectives := []string{
		"golden", "vicious", "sleepy", "brave", "shiny", "mighty", "clever", "wild",
		"happy", "calm", "gentle", "swift", "bold", "mystic", "ancient", "royal",
		"silent", "frosty", "stormy", "quiet", "proud", "noble", "eager", "loyal",
	}
	animals := []string{
		"cavia", "bat", "fox", "owl", "wolf", "bear", "lion", "tiger", "hawk", "eagle",
		"deer", "shark", "whale", "panda", "koala", "lynx", "orca", "seal", "otter", "badger",
		"crow", "raven", "puma", "moth",
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	adj := adjectives[r.Intn(len(adjectives))]
	ani := animals[r.Intn(len(animals))]

	return adj + "-" + ani
}
