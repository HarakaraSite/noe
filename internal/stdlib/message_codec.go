package stdlib

import (
	"errors"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// The codec runs on the owning runtime. It uses messaging's port lookup,
// detach and adopt operations so transfer commits only after cloning succeeds;
// port queues and delivery remain in messaging.go.

// domToken is a DOMException, serialized.
type domToken struct{ name, message, stack string }

// blobToken is a Blob, or a File, serialized: its bytes are copied, as a
// Blob's never change.
type blobToken struct {
	bytes        []byte
	typ          string
	file         bool
	name         string
	lastModified float64
}

// clone implements structuredClone using the same transfer ownership as posting.
func (m *messaging) clone(v quickjs.Value, transfer []quickjs.Value) (quickjs.Value, error) {
	data, ports, err := m.serialize(v, transfer, nil)
	if err != nil {
		return quickjs.Value{}, err
	}
	out, _, err := m.deserialize(data, ports)
	return out, err
}

// newDOMExceptionOr is a DOMException, or an Error if one cannot be made.
func (m *messaging) newDOMExceptionOr(message, name string) quickjs.Value {
	if e, err := m.newDOMException.Call(message, name); err == nil {
		return e
	}
	return m.rt.NewError("Error", message)
}

// serialize serializes v, transferring what transfer lists, for from --
// the port posting it, or nil for structuredClone. The ports transferred are
// this runtime's no longer.
func (m *messaging) serialize(v quickjs.Value, transfer []quickjs.Value, from *portEnd) (any, []*portCore, error) {
	var moved []*portEnd
	codec := &quickjs.CloneCodec{
		Serialize: func(x quickjs.Value) (any, bool, error) {
			kind, err := m.hostKind.Call(x)
			if err != nil || !kind.IsArray() {
				return nil, false, err
			}
			tag, _ := kind.Index(0)
			switch tag.String() {
			case "port":
				return nil, false, m.cloneError("Object that needs transfer was found in message but not listed in transferList")
			case "dom":
				name, _ := kind.Index(1)
				message, _ := kind.Index(2)
				stack, _ := kind.Index(3)
				return domToken{name.String(), message.String(), stack.String()}, true, nil
			case "blob":
				field := func(i int) quickjs.Value { f, _ := kind.Index(i); return f }
				b, _ := field(1).Bytes()
				return blobToken{
					bytes: append([]byte(nil), b...), typ: field(2).String(), file: field(3).Bool(),
					name: field(4).String(), lastModified: field(5).Float(),
				}, true, nil
			}
			return nil, false, nil
		},
		Transfer: func(x quickjs.Value) (any, bool, error) {
			kind, err := m.hostKind.Call(x)
			if err != nil || !kind.IsArray() {
				return nil, false, err
			}
			if tag, _ := kind.Index(0); tag.String() != "port" {
				return nil, false, nil
			}
			id, _ := kind.Index(1)
			e := m.ports[id.Int()]
			switch e {
			case nil:
				return nil, false, m.cloneError("MessagePort in transfer list is already detached")
			case from:
				return nil, false, m.cloneError("Transfer list contains source port")
			}
			for _, p := range moved {
				if p == e {
					return nil, false, m.cloneError("Transfer list contains duplicate MessagePort")
				}
			}
			moved = append(moved, e)
			return e.core, true, nil
		},
	}
	data, err := m.rt.Serialize(v, &quickjs.CloneOptions{Transfer: transfer, Codec: codec})
	if err != nil {
		return nil, nil, m.cloneError(err)
	}
	// Only a clone that succeeded takes the ports: a failure leaves them.
	ports := make([]*portCore, len(moved))
	for i, e := range moved {
		e.detach()
		ports[i] = e.core
	}
	return data, ports, nil
}

// deserialize makes here the value serialize made, and the objects of the
// ports that came with it, in the order they were listed.
func (m *messaging) deserialize(data any, ports []*portCore) (quickjs.Value, []quickjs.Value, error) {
	made := map[*portCore]quickjs.Value{}
	codec := &quickjs.CloneCodec{
		Revive: func(token any) (quickjs.Value, error) {
			switch t := token.(type) {
			case *portCore:
				obj, err := m.adopt(t)
				made[t] = obj
				return obj, err
			case domToken:
				return m.newDOMException.Call(t.message, t.name, t.stack)
			case blobToken:
				return m.makeBlob.Call(m.rt.NewBytes(t.bytes), t.typ, t.file, t.name, t.lastModified)
			}
			return quickjs.Value{}, errors.New("stdlib: an object of an unknown kind was cloned")
		},
	}
	s, ok := data.(*quickjs.Serialized)
	if !ok {
		return quickjs.Value{}, nil, errors.New("stdlib: not a serialized message")
	}
	v, err := m.rt.Deserialize(s, codec)
	if err != nil {
		return quickjs.Value{}, nil, m.cloneError(err)
	}
	objs := make([]quickjs.Value, len(ports))
	for i, p := range ports {
		obj, ok := made[p]
		if !ok {
			// A port posted without appearing in the message is still the
			// receiver's, in the event's ports.
			if obj, err = m.adopt(p); err != nil {
				return quickjs.Value{}, nil, err
			}
		}
		objs[i] = obj
	}
	return v, objs, nil
}

// cloneError is the exception an error from cloning becomes: a DataCloneError
// is a DOMException.
func (m *messaging) cloneError(err any) error {
	var msg string
	switch e := err.(type) {
	case string:
		msg = e
	case error:
		var dce *quickjs.DataCloneError
		if !errors.As(e, &dce) {
			return e
		}
		msg = dce.Message
	}
	exc, err2 := m.newDOMException.Call(msg, "DataCloneError")
	if err2 != nil {
		return err2
	}
	return m.rt.Throw(exc)
}
