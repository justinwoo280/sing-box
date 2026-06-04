package xhttp

// upload_queue is a specialized priorityqueue + channel to reorder generic
// packets by a sequence number

import (
	"container/heap"
	"io"
	"sync"

	E "github.com/sagernet/sing/common/exceptions"
)

type Packet struct {
	Reader  io.ReadCloser
	Payload []byte
	Seq     uint64
}

type uploadQueue struct {
	reader          io.ReadCloser
	nomore          bool
	pushedPackets   chan Packet
	writeCloseMutex sync.Mutex
	heap            uploadHeap
	nextSeq         uint64
	closeOnce       sync.Once
	done            chan struct{}
	maxPackets      int
}

func NewUploadQueue(maxPackets int) *uploadQueue {
	return &uploadQueue{
		pushedPackets: make(chan Packet, maxPackets),
		heap:          uploadHeap{},
		nextSeq:       0,
		done:          make(chan struct{}),
		maxPackets:    maxPackets,
	}
}

func (h *uploadQueue) Push(p Packet) error {
	h.writeCloseMutex.Lock()
	select {
	case <-h.done:
		h.writeCloseMutex.Unlock()
		return E.New("packet queue closed")
	default:
	}
	if h.nomore {
		h.writeCloseMutex.Unlock()
		return E.New("h.reader already exists")
	}
	if p.Reader != nil {
		h.nomore = true
	}
	h.writeCloseMutex.Unlock()
	select {
	case h.pushedPackets <- p:
		return nil
	case <-h.done:
		return E.New("packet queue closed")
	}
}

func (h *uploadQueue) Close() error {
	h.closeOnce.Do(func() {
		close(h.done)
	})
	if h.reader != nil {
		return h.reader.Close()
	}
	return nil
}

func (h *uploadQueue) Read(b []byte) (int, error) {
	if h.reader != nil {
		return h.reader.Read(b)
	}
	if len(h.heap) == 0 {
		select {
		case packet, ok := <-h.pushedPackets:
			if !ok {
				return 0, io.EOF
			}
			if packet.Reader != nil {
				h.reader = packet.Reader
				return h.reader.Read(b)
			}
			heap.Push(&h.heap, packet)
		case <-h.done:
			return 0, io.EOF
		}
	}
	for len(h.heap) > 0 {
		packet := heap.Pop(&h.heap).(Packet)
		n := 0

		if packet.Seq == h.nextSeq {
			copy(b, packet.Payload)
			n = min(len(b), len(packet.Payload))

			if n < len(packet.Payload) {
				// partial read
				packet.Payload = packet.Payload[n:]
				heap.Push(&h.heap, packet)
			} else {
				h.nextSeq = packet.Seq + 1
			}

			return n, nil
		}
		// misordered packet
		if packet.Seq > h.nextSeq {
			if len(h.heap) > h.maxPackets {
				// the "reassembly buffer" is too large, and we want to
				// constrain memory usage somehow. let's tear down the
				// connection, and hope the application retries.
				return 0, E.New("packet queue is too large")
			}
			heap.Push(&h.heap, packet)
			select {
			case packet2, ok := <-h.pushedPackets:
				if !ok {
					return 0, io.EOF
				}
				heap.Push(&h.heap, packet2)
			case <-h.done:
				return 0, io.EOF
			}
		}
	}
	return 0, nil
}

// heap code directly taken from https://pkg.go.dev/container/heap
type uploadHeap []Packet

func (h uploadHeap) Len() int           { return len(h) }
func (h uploadHeap) Less(i, j int) bool { return h[i].Seq < h[j].Seq }
func (h uploadHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *uploadHeap) Push(x any) {
	// Push and Pop use pointer receivers because they modify the slice's length,
	// not the slice itself.
	*h = append(*h, x.(Packet))
}

func (h *uploadHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
