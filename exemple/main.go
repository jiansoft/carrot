package main

import (
	"fmt"
	"log"
	"time"

	"github.com/jiansoft/carrot"
	"github.com/jiansoft/robin"
)

func main() {
	key1 := "rabbit1"
	key2 := "rabbit2"
	val := "I love"

	memory := carrot.New()
	//change expired scan frequency to one second(default one minute)
	memory.SetScanFrequency(time.Second)
	// key1 just lives for one second
	memory.Delay(key1, val, time.Second)
	// key2 is never expired(because param ttl is negative)
	memory.Delay(key2, val, -time.Second)

	robin.Delay(1).Seconds().Do(func() {
		log.Printf("Is memory().%s alive => %v", key1, memory.Have(key1))
		log.Printf("Is memory().%s alive => %v", key2, memory.Have(key2))
	})

	log.Printf("memory.Have(%s) = %v", key1, memory.Have(key1))

	v, ok := memory.Read(key1)
	log.Printf("memory.Read(%s) = %v,%v", key1, ok, v)

	memory.Forget(key1)
	log.Printf("memory.Forget(%s)", key1)
	log.Printf("memory.Have(%s) = %v", key1, memory.Have(key1))
	log.Printf("carrot.Default.Have(%s) = %v", key1, carrot.Default.Have(key1))

	// use carrot default instance
	carrot.Default.Forever(key1, val)
	log.Printf("carrot.Default.Have(%s) = %v", key1, carrot.Default.Have(key1))

	v, ok = carrot.Default.Read(key1)
	log.Printf("carrot.Default.Read(%s) = %v,%v", key1, ok, v)

	carrot.Default.Forget(key1)
	log.Printf("carrot.Default.Have(%s) = %v", key1, carrot.Default.Have(key1))

	robin.Every(9).Seconds().Do(func() {
		//memory.Forever(time.Now().Format("05"), val, carrot.entryOptions{TimeToLive: time.Second})
		ttlKey := "ttl" + time.Now().Format("05")
		memory.Delay(ttlKey, val, 10*time.Second)
		inactiveKey := "inactive" + time.Now().Format("05")
		memory.Inactive(inactiveKey, val, 10*time.Second)
	})
	robin.Every(10).Seconds().Do(func() {
		state := memory.Statistics()
		log.Printf("state %+v", state)
	})

	_, _ = fmt.Scanln()
}
