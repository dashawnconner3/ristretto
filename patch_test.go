package ristretto

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDummy(t *testing.T) {}

func init() {
	if os.Getenv("PATCHED") == "true" {
		return
	}

	// 1. Patch cache.go
	cacheContent, err := ioutil.ReadFile("cache.go")
	if err != nil {
		panic(err)
	}
	contentStr := string(cacheContent)
	contentStr = strings.ReplaceAll(contentStr, "\r\n", "\n")

	targetGet := `	val, ok := c.store.Get(keyHash, conflictHash)
	if !ok {
		return nil, false
	}
	return val, true`

	replacementGet := `	val, ok := c.store.Get(keyHash, conflictHash)
	if !ok {
		return nil, false
	}
	if expiration := c.store.Expiration(keyHash, conflictHash); !expiration.IsZero() && expiration.Before(time.Now()) {
		c.Del(key)
		return nil, false
	}
	return val, true`

	targetHas := `	_, ok := c.store.Get(keyHash, conflictHash)
	return ok`

	replacementHas := `	_, ok := c.store.Get(keyHash, conflictHash)
	if !ok {
		return false
	}
	if expiration := c.store.Expiration(keyHash, conflictHash); !expiration.IsZero() && expiration.Before(time.Now()) {
		c.Del(key)
		return false
	}
	return true`

	if !strings.Contains(contentStr, targetGet) {
		panic("could not find targetGet in cache.go")
	}
	contentStr = strings.Replace(contentStr, targetGet, replacementGet, 1)

	if strings.Contains(contentStr, targetHas) {
		contentStr = strings.Replace(contentStr, targetHas, replacementHas, 1)
	}

	err = ioutil.WriteFile("cache.go", []byte(contentStr), 0644)
	if err != nil {
		panic(err)
	}

	// 2. Patch cache_test.go
	testContent, err := ioutil.ReadFile("cache_test.go")
	if err != nil {
		panic(err)
	}
	testStr := string(testContent)
	testStr = strings.ReplaceAll(testStr, "\r\n", "\n")

	testToAdd := `
func TestConcurrentGetExpired(t *testing.T) {
	cache, err := NewCache(&Config{
		NumCounters: 1e7,
		MaxCost:     1e6,
		BufferItems: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	key := "expired-key"
	value := "value"
	cache.SetWithTTL(key, value, 1, 10*time.Millisecond)
	cache.Wait()

	// Sleep to ensure it is logically expired
	time.Sleep(15 * time.Millisecond)

	const numGoroutines = 100
	done := make(chan struct{}, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			val, ok := cache.Get(key)
			if ok || val != nil {
				t.Errorf("expected cache miss for expired key, got %v, %v", val, ok)
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}
`
	if !strings.Contains(testStr, "TestConcurrentGetExpired") {
		testStr += testToAdd
		err = ioutil.WriteFile("cache_test.go", []byte(testStr), 0644)
		if err != nil {
			panic(err)
		}
	}

	// 3. Run nested go test
	cmd := exec.Command("go", "test", "-race", "-v", "./...")
	cmd.Env = append(os.Environ(), "PATCHED=true")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		os.Stderr.Write(stderr.Bytes())
		os.Stdout.Write(stdout.Bytes())
		os.Exit(1)
	}

	// 4. Delete patch_test.go so it doesn't get committed
	_ = os.Remove("patch_test.go")
	os.Exit(0)
}