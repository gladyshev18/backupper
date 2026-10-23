package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"backupper/internal/chunk"
	"backupper/internal/model"
	"backupper/internal/storage"
)

// dest is one cloud destination of a run.
type dest struct {
	id  string
	st  storage.Storage
	dir string // absolute run folder in that cloud
}

// buf holds one part either in RAM or in a temporary file.
type buf struct {
	mem  []byte
	file *os.File
	n    int64
}

func (b *buf) reader() io.ReaderAt {
	if b.file != nil {
		return b.file
	}
	return bytes.NewReader(b.mem[:b.n])
}

// uploader is an io.Writer that cuts the stream into parts and uploads them
// to every destination while the next part is being filled.
type uploader struct {
	ctx    context.Context
	dests  []dest
	file   string
	size   atomic.Int64
	disk   bool
	tmpDir string
	logf   func(level, format string, a ...any)
	onUp   func(destID string, bytes int64, d time.Duration)
	onPart func(part int)

	free chan *buf
	work chan *buf
	done chan struct{}
	cur  *buf

	mu     sync.Mutex
	err    error
	parts  []model.Part
	partNo int

	hash  hash.Hash
	total int64
}

func newUploader(ctx context.Context, dests []dest, file string, plan model.ChunkPlan, logf func(string, string, ...any)) *uploader {
	u := &uploader{
		ctx:    ctx,
		dests:  dests,
		file:   file,
		disk:   plan.Buffer == model.BufferDisk,
		tmpDir: TmpDir(),
		logf:   logf,
		free:   make(chan *buf, chunk.Buffers),
		work:   make(chan *buf, 1),
		done:   make(chan struct{}),
		hash:   sha256.New(),
	}
	u.size.Store(plan.Size)
	for i := 0; i < chunk.Buffers; i++ {
		u.free <- &buf{}
	}
	go u.loop()
	return u
}

func (u *uploader) failed() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.err
}

func (u *uploader) fail(err error) {
	u.mu.Lock()
	if u.err == nil {
		u.err = err
	}
	u.mu.Unlock()
}

// acquire takes a free buffer and prepares it for the current part size.
// In RAM mode it shrinks the part when the server runs low on memory.
func (u *uploader) acquire() (*buf, error) {
	var b *buf
	select {
	case b = <-u.free:
	case <-u.ctx.Done():
		return nil, u.ctx.Err()
	}
	if err := u.failed(); err != nil {
		u.free <- b
		return nil, err
	}
	b.n = 0
	if u.disk {
		if b.file == nil {
			f, err := os.CreateTemp(u.tmpDir, "backupper-part-*")
			if err != nil {
				return nil, fmt.Errorf("временный файл для куска: %w", err)
			}
			b.file = f
		}
		if err := b.file.Truncate(0); err != nil {
			return nil, err
		}
		_, err := b.file.Seek(0, io.SeekStart)
		return b, err
	}
	size := u.size.Load()
	for size > chunk.MinSize {
		avail := int64(MemAvailable())
		// Memory of buffers we already hold is counted as available.
		if avail <= 0 || avail+int64(cap(b.mem)) >= chunk.Buffers*size+64*chunk.MB {
			break
		}
		size = chunk.Shrink(size)
		u.size.Store(size)
		u.logf("warn", "Мало свободной памяти (%s) — следующий кусок уменьшен до %s", chunk.HumanBytes(avail), chunk.HumanBytes(size))
	}
	if int64(cap(b.mem)) < size {
		b.mem = nil // let GC take the old buffer before allocating
		b.mem = make([]byte, size)
	}
	return b, nil
}

func (u *uploader) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		if err := u.failed(); err != nil {
			return written, err
		}
		if u.cur == nil {
			b, err := u.acquire()
			if err != nil {
				return written, err
			}
			u.cur = b
		}
		size := u.size.Load()
		if !u.disk && int64(cap(u.cur.mem)) < size {
			size = int64(cap(u.cur.mem))
		}
		room := size - u.cur.n
		if room < 0 {
			room = 0
		}
		n := int64(len(p))
		if n > room {
			n = room
		}
		if u.disk {
			if _, err := u.cur.file.Write(p[:n]); err != nil {
				return written, fmt.Errorf("запись временного куска: %w", err)
			}
		} else {
			copy(u.cur.mem[u.cur.n:], p[:n])
		}
		u.hash.Write(p[:n])
		u.cur.n += n
		u.total += n
		written += int(n)
		p = p[n:]
		if u.cur.n >= size {
			if err := u.flush(); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

func (u *uploader) flush() error {
	b := u.cur
	u.cur = nil
	select {
	case u.work <- b:
		return nil
	case <-u.ctx.Done():
		return u.ctx.Err()
	}
}

func (u *uploader) loop() {
	defer close(u.done)
	for b := range u.work {
		if u.failed() == nil {
			if err := u.sendPart(b.reader(), 0, b.n, 0); err != nil {
				u.fail(err)
			}
		}
		u.free <- b
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

func (u *uploader) putRetry(d dest, p string, r io.ReaderAt, off, n int64) error {
	backoff := []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second}
	var err error
	for attempt := 0; ; attempt++ {
		// At least 2 minutes, plus time for a slow 64 KB/s link.
		timeout := 2*time.Minute + time.Duration(n/(64<<10))*time.Second
		ctx, cancel := context.WithTimeout(u.ctx, timeout)
		start := time.Now()
		err = d.st.Put(ctx, p, io.NewSectionReader(r, off, n), n)
		cancel()
		if err == nil {
			if u.onUp != nil {
				u.onUp(d.id, n, time.Since(start))
			}
			return nil
		}
		if u.ctx.Err() != nil {
			return u.ctx.Err()
		}
		if !storage.IsRetryable(err) || attempt >= len(backoff) {
			return err
		}
		u.logf("warn", "%s: ошибка загрузки (%v), повтор через %s", path.Base(p), err, backoff[attempt])
		select {
		case <-time.After(backoff[attempt]):
		case <-u.ctx.Done():
			return u.ctx.Err()
		}
	}
}

// sendPart uploads one part to all destinations. If a destination keeps
// timing out or rejects the size, the part is split in two and later parts
// are made smaller.
func (u *uploader) sendPart(r io.ReaderAt, off, n int64, depth int) error {
	u.mu.Lock()
	u.partNo++
	no := u.partNo
	u.mu.Unlock()
	name := fmt.Sprintf("%s.part%04d", u.file, no)
	if u.onPart != nil {
		u.onPart(no)
	}
	for i, d := range u.dests {
		p := path.Join(d.dir, name)
		err := u.putRetry(d, p, r, off, n)
		if err == nil {
			continue
		}
		if (storage.IsTooLarge(err) || isTimeout(err)) && n >= 2*chunk.MB && depth < 4 {
			for _, prev := range u.dests[:i] {
				_ = prev.st.Delete(u.ctx, path.Join(prev.dir, name))
			}
			u.mu.Lock()
			u.partNo--
			u.mu.Unlock()
			next := chunk.Shrink(u.size.Load())
			u.size.Store(next)
			u.logf("warn", "%s не загружается целиком (%v) — делю пополам, следующие куски по %s", name, err, chunk.HumanBytes(next))
			half := n / 2
			if err := u.sendPart(r, off, half, depth+1); err != nil {
				return err
			}
			return u.sendPart(r, off+half, n-half, depth+1)
		}
		return fmt.Errorf("%s → %s: %w", name, d.id, err)
	}
	u.mu.Lock()
	u.parts = append(u.parts, model.Part{Name: name, Size: n})
	u.mu.Unlock()
	return nil
}

// Finish uploads the last partial part and waits for all uploads.
func (u *uploader) Finish() ([]model.Part, string, error) {
	if u.cur != nil && u.cur.n > 0 && u.failed() == nil {
		if err := u.flush(); err != nil {
			u.fail(err)
		}
	}
	u.putBackCur()
	close(u.work)
	<-u.done
	u.release()
	return u.parts, hex.EncodeToString(u.hash.Sum(nil)), u.failed()
}

// Abort stops the uploader after a failure on the producing side.
func (u *uploader) Abort(err error) {
	u.fail(err)
	u.putBackCur()
	close(u.work)
	<-u.done
	u.release()
}

func (u *uploader) putBackCur() {
	if u.cur != nil {
		u.free <- u.cur
		u.cur = nil
	}
}

func (u *uploader) release() {
	for {
		select {
		case b := <-u.free:
			if b.file != nil {
				b.file.Close()
				os.Remove(b.file.Name())
			}
			b.mem = nil
		default:
			return
		}
	}
}
