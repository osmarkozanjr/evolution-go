// Package store — lock em memória para serializar operações concorrentes do
// ingest (ex. createConversation/createContact por instância+remoteJid,
// equivalente aos "locks Redis em createConversation" da integração antiga).
//
// O conector roda dentro do processo do evolution-go, que é instância única
// (as sessões do WhatsApp vivem no processo), então um lock local é
// suficiente e dispensa Redis.
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Locker adquire e libera locks de curta duração.
type Locker interface {
	// AcquireLock tenta adquirir o lock "key" por até ttl. Se adquirido, retorna
	// true e uma função release() que deve ser chamada (idempotente) para
	// liberar o lock antes do TTL expirar. Se não adquirido, retorna
	// (false, nil, nil) — não é erro, apenas "já está travado por outro".
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, func(), error)
}

type lockEntry struct {
	token   string
	expires time.Time
}

// MemoryLocker implementa Locker em memória, com a mesma semântica do lock
// Redis anterior (SET NX PX + release só pelo dono): o lock expira sozinho
// após o ttl, e release() só libera se o lock ainda pertence a quem adquiriu.
type MemoryLocker struct {
	mu    sync.Mutex
	locks map[string]lockEntry
}

var _ Locker = (*MemoryLocker)(nil)

// NewLocker cria o locker em memória do conector.
func NewLocker() *MemoryLocker {
	return &MemoryLocker{locks: map[string]lockEntry{}}
}

// AcquireLock implementa Locker.
func (l *MemoryLocker) AcquireLock(_ context.Context, key string, ttl time.Duration) (bool, func(), error) {
	token, err := randomToken()
	if err != nil {
		return false, nil, err
	}
	now := time.Now()

	l.mu.Lock()
	if e, held := l.locks[key]; held && now.Before(e.expires) {
		l.mu.Unlock()
		return false, nil, nil
	}
	l.locks[key] = lockEntry{token: token, expires: now.Add(ttl)}
	l.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() {
			l.mu.Lock()
			if e, held := l.locks[key]; held && e.token == token {
				delete(l.locks, key)
			}
			l.mu.Unlock()
		})
	}
	return true, release, nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
