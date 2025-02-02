package pool

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AliyunContainerService/terway/types"
	"github.com/stretchr/testify/assert"
)

type mockObjectFactory struct {
	createDelay   time.Duration
	disposeDeplay time.Duration
	err           error
	totalCreated  int
	totalDisposed int
	idGenerator   int
	lock          sync.Mutex
}

type mockNetworkResource struct {
	id string
}

func (n mockNetworkResource) GetResourceID() string {
	return n.id
}

func (n mockNetworkResource) GetType() string {
	return "mock"
}

func (f *mockObjectFactory) Create() (types.NetworkResource, error) {
	time.Sleep(f.createDelay)
	if f.err != nil {
		return nil, f.err
	}
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.idGenerator == 0 {
		//start from 1000
		f.idGenerator = 1000
	}

	f.idGenerator++
	f.totalCreated++
	return &mockNetworkResource{
		id: fmt.Sprintf("%d", f.idGenerator),
	}, nil
}

func (f *mockObjectFactory) Dispose(types.NetworkResource) error {
	time.Sleep(f.disposeDeplay)
	f.lock.Lock()
	defer f.lock.Unlock()
	f.totalDisposed++
	return f.err
}

func (f *mockObjectFactory) getTotalDisposed() int {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.totalDisposed
}

func (f *mockObjectFactory) getTotalCreated() int {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.totalCreated
}

func TestInitializerWithoutAutoCreate(t *testing.T) {
	factory := &mockObjectFactory{}
	createPool(factory, 3, 0)
	time.Sleep(time.Second)
	assert.Equal(t, 0, factory.getTotalCreated())
	assert.Equal(t, 0, factory.getTotalDisposed())
}

func TestInitializerWithAutoCreate(t *testing.T) {
	factory := &mockObjectFactory{}
	createPool(factory, 0, 0)
	time.Sleep(time.Second)
	assert.Equal(t, 3, factory.getTotalCreated())
	assert.Equal(t, 0, factory.getTotalDisposed())
}

func createPool(factory ObjectFactory, initIdle, initInuse int) ObjectPool {
	id := 0
	cfg := Config{
		Factory: factory,
		Initializer: func(holder ResourceHolder) error {
			for i := 0; i < initIdle; i++ {
				id++
				holder.AddIdle(mockNetworkResource{fmt.Sprintf("%d", id)})
			}
			for i := 0; i < initInuse; i++ {
				id++
				holder.AddInuse(mockNetworkResource{fmt.Sprintf("%d", id)})
			}
			return nil
		},
		MinIdle:  3,
		MaxIdle:  5,
		Capacity: 10,
	}
	pool, err := NewSimpleObjectPool(cfg)
	if err != nil {
		panic(err)
	}
	return pool
}

func TestInitializerExceedMaxIdle(t *testing.T) {
	factory := &mockObjectFactory{}
	createPool(factory, 6, 0)
	time.Sleep(1 * time.Second)
	assert.Equal(t, 0, factory.getTotalCreated())
	assert.Equal(t, 1, factory.getTotalDisposed())
}

func TestInitializerExceedCapacity(t *testing.T) {
	factory := &mockObjectFactory{}
	createPool(factory, 1, 10)
	time.Sleep(time.Second)
	assert.Equal(t, 0, factory.getTotalCreated())
	assert.Equal(t, 1, factory.getTotalDisposed())
}

func TestAcquireIdle(t *testing.T) {
	factory := &mockObjectFactory{}
	pool := createPool(factory, 3, 0)
	_, err := pool.Acquire(context.Background(), "")
	assert.Nil(t, err)
	assert.Equal(t, 0, factory.getTotalCreated())
}
func TestAcquireNonExists(t *testing.T) {
	factory := &mockObjectFactory{}
	pool := createPool(factory, 3, 0)
	_, err := pool.Acquire(context.Background(), "1000")
	assert.Nil(t, err)
	assert.Equal(t, 0, factory.getTotalCreated())
}

func TestAcquireExists(t *testing.T) {
	factory := &mockObjectFactory{}
	pool := createPool(factory, 3, 0)
	res, err := pool.Acquire(context.Background(), "2")
	assert.Nil(t, err)
	assert.Equal(t, 0, factory.getTotalCreated())
	assert.Equal(t, "2", res.GetResourceID())
}

func TestConcurrencyAcquireNoMoreThanCapacity(t *testing.T) {
	factory := &mockObjectFactory{
		createDelay: 2 * time.Millisecond,
	}
	pool := createPool(factory, 1, 0)
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		go func() {
			_, err := pool.Acquire(ctx, "")
			cancel()
			assert.Nil(t, err)
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestConcurrencyAcquireMoreThanCapacity(t *testing.T) {
	factory := &mockObjectFactory{
		createDelay: 2 * time.Millisecond,
	}
	pool := createPool(factory, 3, 0)
	wg := sync.WaitGroup{}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		go func() {
			pool.Acquire(ctx, "")
			cancel()
			wg.Done()
		}()
	}
	wg.Wait()
	assert.Equal(t, 7, factory.getTotalCreated())
}

func TestRelease(t *testing.T) {
	factory := &mockObjectFactory{
		createDelay: 1 * time.Millisecond,
	}
	pool := createPool(factory, 3, 0)
	n1, _ := pool.Acquire(context.Background(), "")
	n2, _ := pool.Acquire(context.Background(), "")
	n3, _ := pool.Acquire(context.Background(), "")
	n4, _ := pool.Acquire(context.Background(), "")
	n5, _ := pool.Acquire(context.Background(), "")
	n6, _ := pool.Acquire(context.Background(), "")
	assert.Equal(t, 3, factory.getTotalCreated())
	pool.Release(n1.GetResourceID())
	pool.Release(n2.GetResourceID())
	pool.Release(n3.GetResourceID())
	time.Sleep(1 * time.Second)
	assert.Equal(t, 0, factory.getTotalDisposed())
	pool.Release(n4.GetResourceID())
	pool.Release(n5.GetResourceID())
	time.Sleep(1 * time.Second)
	assert.Equal(t, 0, factory.getTotalDisposed())
	pool.Release(n6.GetResourceID())
	time.Sleep(1 * time.Second)
	assert.Equal(t, 1, factory.getTotalDisposed())
}

func TestReleaseInvalid(t *testing.T) {
	factory := &mockObjectFactory{}
	pool := createPool(factory, 3, 0)
	err := pool.Release("not-exists")
	assert.Equal(t, err, ErrInvalidState)
}
