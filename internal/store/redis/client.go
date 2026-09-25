package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file is a deliberately small RESP client. The project keeps a single
// third-party runtime dependency (the pure-Go SQLite driver), so rather than
// pulling in go-redis the Redis adapter speaks the wire protocol directly over
// net.Conn. Only the commands the Store interface needs are supported.

// errNil is an internal marker for a RESP nil reply (bulk/array length -1).
var errNil = errors.New("redis: nil reply")

// reply is a decoded RESP value.
type reply struct {
	kind byte // '+', '-', ':', '$', '*'
	str  string
	num  int64
	arr  []reply
	nil  bool
}

// redisError is a RESP error reply ("-ERR ...").
type redisError struct{ msg string }

func (e *redisError) Error() string { return "redis: " + e.msg }

// strings decodes a flat array reply, or a single bulk/simple string, into a
// []string. Nils inside an array become empty strings.
func (r reply) strings() []string {
	switch r.kind {
	case '*':
		out := make([]string, 0, len(r.arr))
		for _, e := range r.arr {
			if e.nil {
				out = append(out, "")
				continue
			}
			out = append(out, e.str)
		}
		return out
	case '$', '+':
		if r.nil {
			return nil
		}
		return []string{r.str}
	default:
		return nil
	}
}

// int64 decodes an integer reply.
func (r reply) int64() (int64, error) {
	switch r.kind {
	case ':':
		return r.num, nil
	case '$', '+':
		if r.nil {
			return 0, errNil
		}
		return strconv.ParseInt(strings.TrimSpace(r.str), 10, 64)
	default:
		return 0, fmt.Errorf("redis: unexpected reply kind %q", r.kind)
	}
}

// conn is one TCP connection to Redis with a buffered reader/writer.
type conn struct {
	c  net.Conn
	br *bufio.Reader
	bw *bufio.Writer
}

func (c *conn) close() {
	_ = c.c.Close()
}

// do sends one command and reads its reply. A transport failure is returned as
// a non-nil error (the caller should discard the connection); a Redis-level
// error (RESP '-') is returned as *redisError (the connection stays usable).
func (c *conn) do(ctx context.Context, args ...string) (reply, error) {
	if err := c.writeCommand(args); err != nil {
		return reply{}, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.c.SetDeadline(deadline)
	} else {
		_ = c.c.SetDeadline(time.Time{})
	}
	if err := c.bw.Flush(); err != nil {
		return reply{}, err
	}
	r, err := c.readReply()
	if err != nil {
		return reply{}, err
	}
	if r.kind == '-' {
		return r, &redisError{msg: r.str}
	}
	return r, nil
}

// doMulti wraps commands in MULTI/EXEC so they apply atomically. It returns the
// per-command replies (EXEC's array), or an error if EXEC aborted.
func (c *conn) doMulti(ctx context.Context, cmds [][]string) ([]reply, error) {
	if len(cmds) == 0 {
		return nil, nil
	}
	if err := c.writeCommand([]string{"MULTI"}); err != nil {
		return nil, err
	}
	for _, cmd := range cmds {
		if err := c.writeCommand(cmd); err != nil {
			return nil, err
		}
	}
	if err := c.writeCommand([]string{"EXEC"}); err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.c.SetDeadline(deadline)
	} else {
		_ = c.c.SetDeadline(time.Time{})
	}
	if err := c.bw.Flush(); err != nil {
		return nil, err
	}
	// MULTI and each queued command reply with +QUEUED.
	if _, err := c.readReply(); err != nil {
		return nil, err
	}
	for range cmds {
		if _, err := c.readReply(); err != nil {
			return nil, err
		}
	}
	r, err := c.readReply()
	if err != nil {
		return nil, err
	}
	if r.kind == '-' {
		return nil, &redisError{msg: r.str}
	}
	if r.nil {
		return nil, errors.New("redis: transaction aborted (EXEC returned nil)")
	}
	// A command queued in MULTI can still fail at EXEC time (e.g. WRONGTYPE).
	// Redis reports it as an error element in the EXEC array; surface the first
	// one instead of silently discarding the failure.
	for _, e := range r.arr {
		if e.kind == '-' {
			return r.arr, &redisError{msg: e.str}
		}
	}
	return r.arr, nil
}

func (c *conn) writeCommand(args []string) error {
	if _, err := c.bw.WriteString("*" + strconv.Itoa(len(args)) + "\r\n"); err != nil {
		return err
	}
	for _, a := range args {
		if _, err := c.bw.WriteString("$" + strconv.Itoa(len(a)) + "\r\n"); err != nil {
			return err
		}
		if _, err := c.bw.WriteString(a); err != nil {
			return err
		}
		if _, err := c.bw.WriteString("\r\n"); err != nil {
			return err
		}
	}
	return nil
}

func (c *conn) readReply() (reply, error) {
	line, err := c.readLine()
	if err != nil {
		return reply{}, err
	}
	if len(line) == 0 {
		return reply{}, errors.New("redis: empty reply line")
	}
	kind, body := line[0], line[1:]
	switch kind {
	case '+':
		return reply{kind: '+', str: body}, nil
	case '-':
		return reply{kind: '-', str: body}, nil
	case ':':
		n, err := strconv.ParseInt(body, 10, 64)
		if err != nil {
			return reply{}, fmt.Errorf("redis: bad integer reply %q", body)
		}
		return reply{kind: ':', num: n}, nil
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			return reply{}, fmt.Errorf("redis: bad bulk length %q", body)
		}
		if n < 0 {
			return reply{kind: '$', nil: true}, nil
		}
		buf := make([]byte, n+2) // include trailing CRLF
		if _, err := io.ReadFull(c.br, buf); err != nil {
			return reply{}, err
		}
		return reply{kind: '$', str: string(buf[:n])}, nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			return reply{}, fmt.Errorf("redis: bad array length %q", body)
		}
		if n < 0 {
			return reply{kind: '*', nil: true}, nil
		}
		arr := make([]reply, 0, n)
		for i := 0; i < n; i++ {
			e, err := c.readReply()
			if err != nil {
				return reply{}, err
			}
			arr = append(arr, e)
		}
		return reply{kind: '*', arr: arr}, nil
	default:
		return reply{}, fmt.Errorf("redis: unexpected reply type %q", kind)
	}
}

func (c *conn) readLine() (string, error) {
	line, err := c.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

// pool is a tiny mutex-guarded connection pool. A connection that fails is
// discarded rather than returned, so the next caller dials a fresh one.
type pool struct {
	addr     string
	password string
	db       int
	dialer   net.Dialer

	mu    sync.Mutex
	conns []*conn
}

func newPool(addr, password string, db int) *pool {
	return &pool{addr: addr, password: password, db: db, dialer: net.Dialer{Timeout: 5 * time.Second}}
}

func (p *pool) get(ctx context.Context) (*conn, error) {
	p.mu.Lock()
	if n := len(p.conns); n > 0 {
		c := p.conns[n-1]
		p.conns = p.conns[:n-1]
		p.mu.Unlock()
		return c, nil
	}
	p.mu.Unlock()
	return p.dial(ctx)
}

func (p *pool) put(c *conn) {
	p.mu.Lock()
	// Keep the pool small; Redis connections are cheap to reopen.
	if len(p.conns) < 8 {
		p.conns = append(p.conns, c)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	c.close()
}

func (p *pool) dial(ctx context.Context) (*conn, error) {
	nc, err := p.dialer.DialContext(ctx, "tcp", p.addr)
	if err != nil {
		return nil, fmt.Errorf("redis: dial %s: %w", p.addr, err)
	}
	c := &conn{c: nc, br: bufio.NewReader(nc), bw: bufio.NewWriter(nc)}
	if p.password != "" {
		if _, err := c.do(ctx, "AUTH", p.password); err != nil {
			c.close()
			return nil, fmt.Errorf("redis: auth: %w", err)
		}
	}
	if p.db != 0 {
		if _, err := c.do(ctx, "SELECT", strconv.Itoa(p.db)); err != nil {
			c.close()
			return nil, fmt.Errorf("redis: select db %d: %w", p.db, err)
		}
	}
	return c, nil
}

func (p *pool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.close()
	}
	p.conns = nil
}
