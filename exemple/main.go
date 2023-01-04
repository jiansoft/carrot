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
	memory.KeepDelay(key1, val, time.Second)
	// key2 is never expired(because param ttl is negative)
	memory.KeepDelay(key2, val, -time.Second)

	robin.Delay(1).Seconds().Do(func() {
		log.Printf("Is memory().%s alive => %v", key1, memory.Have(key1))
		log.Printf("Is memory().%s alive => %v", key2, memory.Have(key2))
	})

	log.Printf("memory.Have(%s) = %v", key1, memory.Have(key1))

	v, ok := memory.Read(key1)
	log.Printf("memory.Read(%s) = %v,%v", key1, ok, v)

	memory.Forget(key1)
	log.Printf("memory.Have(%s) = %v", key1, memory.Have(key1))
	log.Printf("carrot.Default.Have(%s) = %v", key1, carrot.Default.Have(key1))

	/* carrot.Default.Keep(key1, val, time.Second)
	   log.Printf("carrot.Default.Have(%s) = %v", key1, carrot.Default.Have(key1))

	   v, ok = carrot.Default.Read(key1)
	   log.Printf("carrot.Default.Read(%s) = %v,%v", key1, ok, v)

	   carrot.Default.Forget(key1)
	   log.Printf("carrot.Default.Have(%s) = %v", key1, carrot.Default.Have(key1))

	   robin.Every(1).Seconds().Do(func() {
	       memory.Keep(time.Now().Format("05"), val, time.Hour)
	   })*/
	_, _ = fmt.Scanln()
}
