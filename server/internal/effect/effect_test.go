package effect

import (
	"context"
	"testing"

	"github.com/blakej11/cricket/internal/fileset"
	"github.com/blakej11/cricket/internal/lease"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/types"
)

// Since the "sound" and "light" types are constructed within the
// effect subsystem, this testing code can't set values inside them.
// This serves as a way of stashing the expected results in a known
// place -- good enough for simple single-threaded tests.
type effectResult struct {
	t	*testing.T
	id	types.ID
	param	int
	file	fileset.File
}

var effectResults []effectResult

func pushEffectResult(e effectResult) {
	effectResults = append(effectResults, e)
}

func popEffectResult() effectResult {
	if len(effectResults) == 0 {
		log.Panicf("effectResults is empty\n")
	}
	e := effectResults[0]
	effectResults = effectResults[1:]
	return e
}

type sound struct {}

// Fields have to be capitalized so they can be updated via reflection.
type s1p struct {
	Sound1p	*random.Variable
}
type s2p struct {
	Sound2p	*random.Variable
}
type s1fs struct {
	Sound1fs *fileset.Set
}
type s2fs struct {
	Sound2fs *fileset.Set
}

func (s sound) Run(ctx context.Context, ids types.IDSetConsumer, params any, fileSets any) {
	e := popEffectResult()

	i := ids.Snapshot()
	if len(i) != 1 {
		e.t.Errorf("sound.Run wanted 1 ID, got %v\n", i)
	} else if i[0] != e.id {
		e.t.Errorf("sound.Run wanted id %v, got %v\n", e.id, i[0])
	}

	gotParam := 0
	if s1p, ok := params.(*s1p); ok {
		gotParam = s1p.Sound1p.Int()
	} else if s2p, ok := params.(*s2p); ok {
		gotParam = s2p.Sound2p.Int()
	}
	if gotParam != e.param {
		e.t.Errorf("sound.Run wanted param %d, got %d\n", e.param, gotParam)
	}

	gotFile := fileset.File{}
	if s1fs, ok := fileSets.(*s1fs); ok {
		gotFile = s1fs.Sound1fs.Pick()
	} else if s2fs, ok := fileSets.(*s2fs); ok {
		gotFile = s2fs.Sound2fs.Pick()
	}
	if gotFile != e.file {
		e.t.Errorf("sound.Run wanted file %v, got %v\n", e.file, gotFile)
	}
}

type light struct {}

type l1p struct {
	Light1p	*random.Variable
}
type l2p struct {
	Light2p	*random.Variable
}

func (l light) Run(ctx context.Context, ids types.IDSetConsumer, params any, _ any) {
	e := popEffectResult()

	i := ids.Snapshot()
	if len(i) != 1 {
		e.t.Errorf("light.Run wanted 1 ID, got %v\n", i)
	} else if i[0] != e.id {
		e.t.Errorf("light.Run wanted id %v, got %v\n", e.id, i[0])
	}

	gotParam := 0
	if l1p, ok := params.(*l1p); ok {
		gotParam = l1p.Light1p.Int()
	} else if l2p, ok := params.(*l2p); ok {
		gotParam = l2p.Light2p.Int()
	}
	if gotParam != e.param {
		e.t.Errorf("light.Run wanted param %d, got %d\n", e.param, gotParam)
	}
}

func TestEffects(t *testing.T) {
	RegisterSound[sound, s1p, s1fs]("sound1")
	RegisterSound[sound, s2p, s2fs]("sound2")
	RegisterLight[light, l1p]("light1")
	RegisterLight[light, l2p]("light2")

	sound1c := Config{
		Algorithm: "sound1",
		FileSets: map[string]string{
			"Sound1fs": "fs1",
		},
		Parameters: map[string]random.Config{
			"Sound1p": random.FixedConfig(1),
		},
		Duration: random.FixedConfig(10),
		Lease: lease.Config{
			Type:          types.Sound,
			FleetFraction: random.FixedConfig(1),
		},
	}
	file1 := fileset.File{
		Folder: 1,
		File: 1,
		Duration: 10.0,
	}
	fs1, err := fileset.New(
		"fs1",
		fileset.Config{Regex: ".*"},
		map[string]fileset.File{"a": file1},
	)
	if err != nil {
		t.Fatalf("failed to create fileset 1: %v", err)
	}
	sound1e, err := newEffect("sound1", sound1c, map[string]*fileset.Set{"fs1": fs1})
	if err != nil {
		t.Errorf("failed to create sound effect 1: %v", err)
	}
	sound1e.setSkipDrain()

	sound2c := Config{
		Algorithm: "sound2",
		FileSets: map[string]string{
			"Sound2fs": "fs2",
		},
		Parameters: map[string]random.Config{
			"Sound2p": random.FixedConfig(2),
		},
		Duration: random.FixedConfig(20),
		Lease: lease.Config{
			Type:          types.Sound,
			FleetFraction: random.FixedConfig(1),
		},
	}
	file2 := fileset.File{
		Folder: 2,
		File: 2,
		Duration: 20.0,
	}
	fs2, err := fileset.New(
		"fs2",
		fileset.Config{Regex: ".*"},
		map[string]fileset.File{"b": file2},
	)
	if err != nil {
		t.Fatalf("failed to create fileset 2: %v", err)
	}
	sound2e, err := newEffect("sound2", sound2c, map[string]*fileset.Set{"fs2": fs2})
	if err != nil {
		t.Errorf("failed to create sound effect 2: %v", err)
	}
	sound2e.setSkipDrain()

	light1c := Config{
		Algorithm: "light1",
		Parameters: map[string]random.Config{
			"Light1p": random.FixedConfig(1),
		},
		Duration: random.FixedConfig(10),
		Lease: lease.Config{
			Type:          types.Light,
			FleetFraction: random.FixedConfig(1),
		},
	}
	light1e, err := newEffect("light1", light1c, nil)
	if err != nil {
		t.Errorf("failed to create light effect 1: %v", err)
	}
	light1e.setSkipDrain()

	light2c := Config{
		Algorithm: "light2",
		Parameters: map[string]random.Config{
			"Light2p": random.FixedConfig(2),
		},
		Duration: random.FixedConfig(20),
		Lease: lease.Config{
			Type:          types.Light,
			FleetFraction: random.FixedConfig(1),
		},
	}
	light2e, err := newEffect("light2", light2c, nil)
	if err != nil {
		t.Errorf("failed to create light effect 2: %v", err)
	}
	light2e.setSkipDrain()

	pushEffectResult(effectResult{
		t:	t,
		id:	types.ID("s1"),
		param:	1,
		file:	file1,
	})
	sound1e.run(types.NewFixedIDSet(types.ID("s1")))

	pushEffectResult(effectResult{
		t:	t,
		id:	types.ID("s2"),
		param:	2,
		file:	file2,
	})
	sound2e.run(types.NewFixedIDSet(types.ID("s2")))

	pushEffectResult(effectResult{
		t:	t,
		id:	types.ID("l1"),
		param:	1,
	})
	light1e.run(types.NewFixedIDSet(types.ID("l1")))

	pushEffectResult(effectResult{
		t:	t,
		id:	types.ID("l2"),
		param:	2,
	})
	light2e.run(types.NewFixedIDSet(types.ID("l2")))
}

type dup struct {
	count int
}

type dupParams struct {}

func (d dup) Run(ctx context.Context, _ types.IDSetConsumer, _ any, _ any) {
	e := popEffectResult()
	if d.count > 0 {
		e.t.Errorf("dup: wanted count 0, got %d\n", d.count)
	}
	d.count++
}

func TestUseSameAlgTwice(t *testing.T) {
	RegisterLight[dup, dupParams]("dup")

	d1c := Config{
		Algorithm: "dup",
		Parameters: map[string]random.Config{},
		Duration: random.FixedConfig(10),
		Lease: lease.Config{
			Type:          types.Light,
			FleetFraction: random.FixedConfig(0.3),
		},
	}
	d1e, err := newEffect("dup", d1c, nil)
	if err != nil {
		t.Errorf("failed to create light effect 1: %v", err)
	}
	d1e.setSkipDrain()

	d2c := Config{
		Algorithm: "dup",
		Parameters: map[string]random.Config{},
		Duration: random.FixedConfig(10),
		Lease: lease.Config{
			Type:          types.Light,
			FleetFraction: random.FixedConfig(0.7),
		},
	}
	d2e, err := newEffect("dup", d2c, nil)
	if err != nil {
		t.Errorf("failed to create light effect 2: %v", err)
	}
	d2e.setSkipDrain()

	pushEffectResult(effectResult{t: t})
	d1e.run(types.NewFixedIDSet(types.ID("s1")))

	pushEffectResult(effectResult{t: t})
	d2e.run(types.NewFixedIDSet(types.ID("s2")))
}
