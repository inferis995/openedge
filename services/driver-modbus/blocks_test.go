package main

import (
	"fmt"
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// createBlocks decides which registers are fetched in which request, and where
// inside the reply each tag's bytes are. An error here does not fail: it reads
// a real register and hands it to the wrong tag, so a pressure gauge shows a
// motor's speed and both numbers are in range.

func tg(id int, alias, code, dtype string) models.Tag {
	return models.Tag{ID: id, Alias: alias, Code: code, DataType: dtype}
}

// blockFor finds the block a tag ended up in.
func blockFor(blocks []Block, tagID int) (Block, TagInBlock, bool) {
	for i := range blocks {
		for j := range blocks[i].Tags {
			if blocks[i].Tags[j].Tag.ID == tagID {
				return blocks[i], blocks[i].Tags[j], true
			}
		}
	}
	return Block{}, TagInBlock{}, false
}

// The property that matters most: for every tag, the bytes the poll loop will
// slice out of the reply are the bytes of that tag's own address.
func TestEveryTagCanBeFoundInsideItsBlock(t *testing.T) {
	d := &Driver{}
	tags := []models.Tag{
		tg(1, "temp", "40001", "INT"),
		tg(2, "press", "40002", "REAL"),
		tg(3, "count", "40010", "DINT"),
		tg(4, "run", "00001", "BOOL"),
		tg(5, "far", "40200", "INT"),
	}
	blocks := d.createBlocks(tags, false)

	for _, tag := range tags {
		b, tib, ok := blockFor(blocks, tag.ID)
		if !ok {
			t.Errorf("tag %q is in no block at all: it will never be read and "+
				"will publish nothing, with no error anywhere", tag.Alias)
			continue
		}
		if tib.Offset < b.StartAddress {
			t.Errorf("tag %q sits before the start of its block (%d < %d): the "+
				"poll loop would slice at a negative offset", tag.Alias, tib.Offset, b.StartAddress)
		}
		if tib.Offset >= b.StartAddress+b.Count {
			t.Errorf("tag %q sits past the end of its block (%d >= %d+%d): its "+
				"bytes are not in the reply", tag.Alias, tib.Offset, b.StartAddress, b.Count)
		}
	}
}

// A block must cover the LAST register of every tag in it, not just the first.
// A DINT or a REAL occupies two registers, and a block sized for one leaves
// the second outside the reply.
func TestABlockCoversTheWholeOfEveryTagInIt(t *testing.T) {
	d := &Driver{}
	blocks := d.createBlocks([]models.Tag{
		tg(1, "flow", "40001", "REAL"),  // registers 0 and 1
		tg(2, "total", "40003", "DINT"), // registers 2 and 3
	}, false)

	if len(blocks) != 1 {
		t.Fatalf("two adjacent tags produced %d blocks, want 1", len(blocks))
	}
	b := blocks[0]
	if b.Count < 4 {
		t.Fatalf("the block reads %d registers but its tags occupy 4; the second "+
			"register of the last tag is outside the reply and it will decode "+
			"whatever follows", b.Count)
	}
}

// Holding registers, input registers, coils and discrete inputs are separate
// address spaces read with different function codes. Mixing them into one
// request reads the wrong space entirely.
func TestDifferentAddressSpacesNeverShareABlock(t *testing.T) {
	d := &Driver{}
	// The offsets are deliberately CONSECUTIVE across the four spaces —
	// holding 0, input 1, coil 2, discrete 3 — so nothing but the address-space
	// check can keep them apart. With addresses that all land on offset 0 the
	// blocks come out separate anyway, because the gap arithmetic underflows,
	// and the test passes with the check deleted.
	blocks := d.createBlocks([]models.Tag{
		tg(1, "hold", "40001", "INT"),      // holding, offset 0
		tg(2, "input", "30002", "INT"),     // input,   offset 1
		tg(3, "coil", "00003", "BOOL"),     // coil,    offset 2
		tg(4, "discrete", "10004", "BOOL"), // discrete, offset 3
	}, false)

	for _, b := range blocks {
		if len(b.Tags) > 1 {
			t.Errorf("block of type %q holds %d tags; tags from different "+
				"address spaces would be read with one function code and return "+
				"another space's registers", b.DataType, len(b.Tags))
		}
	}

	for _, b := range blocks {
		for _, tib := range b.Tags {
			got, _, _ := blockFor(blocks, tib.Tag.ID)
			if got.DataType != b.DataType {
				t.Fatalf("inconsistent grouping for %q", tib.Tag.Alias)
			}
		}
	}
	if len(blocks) != 4 {
		t.Fatalf("four different address spaces produced %d blocks, want 4 — a "+
			"shared block would read one space with another's function code", len(blocks))
	}
}

// A small gap is filled so two nearby tags cost one request instead of two.
func TestASmallGapIsBridgedIntoOneRequest(t *testing.T) {
	d := &Driver{}
	blocks := d.createBlocks([]models.Tag{
		tg(1, "a", "40001", "INT"),
		tg(2, "b", "40005", "INT"), // three registers of gap
	}, false)
	if len(blocks) != 1 {
		t.Fatalf("two tags four registers apart produced %d requests, want 1", len(blocks))
	}
}

// A large gap is not, or one tag at register 0 and another at register 9000
// would ask the device for 9000 registers and be refused — taking both tags
// down with it.
func TestALargeGapIsNotBridged(t *testing.T) {
	d := &Driver{}
	blocks := d.createBlocks([]models.Tag{
		tg(1, "a", "40001", "INT"),
		tg(2, "b", "40500", "INT"),
	}, false)
	if len(blocks) != 2 {
		t.Fatalf("tags 500 registers apart produced %d requests, want 2", len(blocks))
	}
	for _, b := range blocks {
		if b.Count > 125 {
			t.Errorf("a block asks for %d registers; Modbus allows 125 and the "+
				"device will refuse the whole request", b.Count)
		}
	}
}

// No block may exceed what a Modbus device will answer. A block one register
// over the limit is refused in its entirety, so every tag in it goes bad at
// once — which reads as a dead PLC rather than as a configuration error.
func TestNoBlockExceedsWhatADeviceWillAnswer(t *testing.T) {
	d := &Driver{}
	var tags []models.Tag
	for i := 0; i < 200; i++ {
		tags = append(tags, tg(i+1, fmt.Sprintf("t%d", i), fmt.Sprintf("%d", 40001+i), "INT"))
	}
	blocks := d.createBlocks(tags, false)
	if len(blocks) < 2 {
		t.Fatalf("200 consecutive registers were put into %d block(s)", len(blocks))
	}
	for _, b := range blocks {
		if b.Count > 125 {
			t.Errorf("block starting at %d asks for %d registers, over the 125 "+
				"a device will answer", b.StartAddress, b.Count)
		}
	}
	// and every tag still landed somewhere
	for _, tag := range tags {
		if _, _, ok := blockFor(blocks, tag.ID); !ok {
			t.Fatalf("tag %q was lost while splitting blocks", tag.Alias)
		}
	}
}

// A tag whose address cannot be parsed is left out of every block — it has no
// registers to read. What must not happen is the rest of the gateway going
// with it.
func TestOneUnparseableAddressDoesNotCostTheOtherTags(t *testing.T) {
	d := &Driver{}
	blocks := d.createBlocks([]models.Tag{
		tg(1, "good", "40001", "INT"),
		tg(2, "rubbish", "not-an-address", "INT"),
		tg(3, "alsogood", "40002", "INT"),
	}, false)

	for _, id := range []int{1, 3} {
		if _, _, ok := blockFor(blocks, id); !ok {
			t.Errorf("tag %d was dropped because another tag's address was bad", id)
		}
	}
	if _, _, ok := blockFor(blocks, 2); ok {
		t.Error("a tag with an unparseable address was given registers to read")
	}
}

func TestNoTagsProducesNoBlocks(t *testing.T) {
	d := &Driver{}
	if got := d.createBlocks(nil, false); len(got) != 0 {
		t.Fatalf("got %d blocks for no tags", len(got))
	}
	if got := d.createBlocks([]models.Tag{tg(1, "x", "nonsense", "INT")}, false); len(got) != 0 {
		t.Fatalf("got %d blocks when the only tag had no usable address; the "+
			"poll loop would issue a request for nothing", len(got))
	}
}
