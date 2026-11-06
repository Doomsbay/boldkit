package split

import (
	"crypto/md5"
	"fmt"
	"math"
	"sort"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/Doomsbay/BoldKit/internal/qc"
	"github.com/Doomsbay/BoldKit/internal/util"
)

type SplitStrategy struct {
	MinSeenRecords        int
	MinSeenBarcodes       int
	MaxTestTargetPerClass int
	SeenTestTarget        float64
	SeenValTarget         float64
	UnseenValTarget       float64
	HeldoutFrac           float64
	IncludeSpecies        []string
	HoldoutSpecies        []string
}

func DefaultStrategy() SplitStrategy {
	return SplitStrategy{
		MinSeenRecords:        8,
		MinSeenBarcodes:       2,
		MaxTestTargetPerClass: 25,
		SeenTestTarget:        0.20,
		SeenValTarget:         0.05,
		UnseenValTarget:       0.20,
		HeldoutFrac:           0.50,
	}
}

func (s SplitStrategy) isZero() bool {
	return s.MinSeenRecords == 0 && s.MinSeenBarcodes == 0 && s.MaxTestTargetPerClass == 0 &&
		s.SeenTestTarget == 0 && s.SeenValTarget == 0 && s.UnseenValTarget == 0 && s.HeldoutFrac == 0 &&
		len(s.IncludeSpecies) == 0 && len(s.HoldoutSpecies) == 0
}

func (s SplitStrategy) validate() error {
	if s.MinSeenRecords < 0 || s.MinSeenBarcodes < 0 || s.MaxTestTargetPerClass < 0 {
		return fmt.Errorf("split strategy record and barcode thresholds must be non-negative")
	}
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"seen test fraction", s.SeenTestTarget},
		{"seen validation fraction", s.SeenValTarget},
		{"unseen validation fraction", s.UnseenValTarget},
		{"heldout fraction", s.HeldoutFrac},
	} {
		if f.value < 0 || f.value > 1 {
			return fmt.Errorf("split strategy %s must be within [0,1]: %g", f.name, f.value)
		}
	}
	return nil
}

type barcodeUnit struct {
	hash  [16]byte
	count int
}

type barcodeGroup struct {
	label    string
	count    int
	conflict bool
}

type splitPlan struct {
	seqBucket  map[[16]byte]string
	conflicted map[[16]byte]struct{}
	invalidIDs map[string]struct{}
	canonical  bool
}

type splitTarget struct {
	bucket string
	target int
}

type conflictReport struct {
	hashes  map[[16]byte]struct{}
	groups  int
	records int
}

func permill(frac float64) int {
	return int(math.Round(frac * 1000))
}

func heldoutHashThreshold(frac float64) int {
	return ceilDiv(permill(frac)*256, 1000)
}

func detectSequenceLabelConflicts(input string, labels map[string]string, invalidIDs map[string]struct{}, gapPolicy string, skipDuplicateIDs bool) (conflictReport, error) {
	in, err := boldio.OpenInput(input)
	if err != nil {
		return conflictReport{}, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	groups := make(map[[16]byte]barcodeGroup)
	seenIDs := make(map[string]struct{})
	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		if _, bad := invalidIDs[rec.ID]; bad {
			return nil
		}
		label, ok := labels[rec.ID]
		if !ok {
			return nil
		}
		if skipDuplicateIDs {
			if _, dup := seenIDs[rec.ID]; dup {
				return nil
			}
			seenIDs[rec.ID] = struct{}{}
		}
		clean, _ := qc.CleanSequenceForPolicy(gapPolicy, rec.Seq)
		if len(clean) == 0 {
			return nil
		}
		hash := md5.Sum(clean)
		group := groups[hash]
		if group.count == 0 {
			group.label = label
		} else if group.label != label {
			group.conflict = true
		}
		group.count++
		groups[hash] = group
		return nil
	})
	if err != nil {
		return conflictReport{}, err
	}

	report := conflictReport{hashes: make(map[[16]byte]struct{})}
	for hash, group := range groups {
		if !group.conflict {
			continue
		}
		report.hashes[hash] = struct{}{}
		report.groups++
		report.records += group.count
	}
	return report, nil
}

func buildSplitPlan(input string, labels map[string]string, invalidIDs map[string]struct{}, preConflicted map[[16]byte]struct{}, strategy SplitStrategy, canonical bool) (splitPlan, splitStats, error) {
	if err := strategy.validate(); err != nil {
		return splitPlan{}, splitStats{}, err
	}
	in, err := boldio.OpenInput(input)
	if err != nil {
		return splitPlan{}, splitStats{}, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	barcodeGroups := make(map[[16]byte]barcodeGroup, 1<<20)
	stats := splitStats{}
	hasher := barcodeHasher{canonical: canonical}

	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		stats.TotalRecords++
		if _, bad := invalidIDs[rec.ID]; bad {
			return nil
		}
		label, ok := labels[rec.ID]
		if !ok {
			invalidIDs[rec.ID] = struct{}{}
			return nil
		}

		hash := hasher.hash(rec.Seq)
		group := barcodeGroups[hash]
		if group.count == 0 {
			group.label = label
		} else if group.label != label {
			group.conflict = true
		}
		group.count++
		barcodeGroups[hash] = group
		return nil
	})
	if err != nil {
		return splitPlan{}, splitStats{}, err
	}

	seqBucket := make(map[[16]byte]string, len(barcodeGroups))
	conflicted := make(map[[16]byte]struct{})
	speciesUnits := make(map[string][]barcodeUnit)
	speciesCounts := make(map[string]int)

	conflictRecords := 0
	for hash, group := range barcodeGroups {
		if group.conflict {
			conflicted[hash] = struct{}{}
			conflictRecords += group.count
			continue
		}
		if _, bad := preConflicted[hash]; bad {
			conflicted[hash] = struct{}{}
			conflictRecords += group.count
			continue
		}
		speciesUnits[group.label] = append(speciesUnits[group.label], barcodeUnit{hash: hash, count: group.count})
		speciesCounts[group.label] += group.count
	}

	unseenHashBound := 256 - heldoutHashThreshold(strategy.HeldoutFrac)
	includeSet := speciesSet(strategy.IncludeSpecies)
	holdoutSet := speciesSet(strategy.HoldoutSpecies)
	for name := range includeSet {
		if _, ok := speciesUnits[name]; !ok {
			stats.IncludeUnknown++
		}
	}
	for name := range holdoutSet {
		if _, ok := speciesUnits[name]; !ok {
			stats.HoldoutUnknown++
		}
	}
	if stats.IncludeUnknown > 0 || stats.HoldoutUnknown > 0 {
		util.Logf("split: %d include and %d holdout species not found among input species", stats.IncludeUnknown, stats.HoldoutUnknown)
	}

	stats.TotalClasses = len(speciesUnits)
	for label, units := range speciesUnits {
		total := speciesCounts[label]
		uniqueBarcodes := len(units)
		sort.Slice(units, func(i, j int) bool {
			return lessHash(units[i].hash, units[j].hash)
		})

		if _, forced := includeSet[label]; forced {
			stats.SeenClasses++
			stats.ForcedSeenClasses++
			testTarget := minInt(strategy.MaxTestTargetPerClass, ceilDiv(permill(strategy.SeenTestTarget)*total, 1000))
			valTarget := ceilDiv(permill(strategy.SeenValTarget)*(total-testTarget), 1000)
			assignUnits(seqBucket, units, []splitTarget{
				{bucket: bucketSeenTest, target: testTarget},
				{bucket: bucketSeenVal, target: valTarget},
				{bucket: bucketSeenTrain, target: -1},
			})
			guaranteeTrainingUnit(seqBucket, units)
			continue
		}

		if _, forced := holdoutSet[label]; forced {
			stats.HeldoutClasses++
			stats.ForcedHeldoutClasses++
			for _, unit := range units {
				seqBucket[unit.hash] = bucketHeldout
			}
			continue
		}

		if total >= strategy.MinSeenRecords && uniqueBarcodes >= strategy.MinSeenBarcodes {
			stats.SeenClasses++
			testTarget := minInt(strategy.MaxTestTargetPerClass, ceilDiv(permill(strategy.SeenTestTarget)*total, 1000))
			valTarget := ceilDiv(permill(strategy.SeenValTarget)*(total-testTarget), 1000)
			assignUnits(seqBucket, units, []splitTarget{
				{bucket: bucketSeenTest, target: testTarget},
				{bucket: bucketSeenVal, target: valTarget},
				{bucket: bucketSeenTrain, target: -1},
			})
			continue
		}

		if int(classHashByte(label)) < unseenHashBound {
			stats.UnseenClasses++
			testTarget := minInt(strategy.MaxTestTargetPerClass, ceilDiv(permill(strategy.SeenTestTarget)*total, 1000))
			valTarget := ceilDiv(permill(strategy.UnseenValTarget)*(total-testTarget), 1000)
			assignUnits(seqBucket, units, []splitTarget{
				{bucket: bucketUnseenTest, target: testTarget},
				{bucket: bucketUnseenVal, target: valTarget},
				{bucket: bucketUnseenKeys, target: -1},
			})
			continue
		}

		stats.HeldoutClasses++
		for _, unit := range units {
			seqBucket[unit.hash] = bucketHeldout
		}
	}

	if len(conflicted) > 0 {
		util.Logf("split: %d barcode groups span multiple species labels (moved to %s)", len(conflicted), bucketPretrain)
	}
	stats.ConflictGroups = len(conflicted)
	stats.ConflictRecords = conflictRecords

	return splitPlan{
		seqBucket:  seqBucket,
		conflicted: conflicted,
		invalidIDs: invalidIDs,
		canonical:  canonical,
	}, stats, nil
}

type barcodeHasher struct {
	canonical bool
	scratch   []byte
}

func (h *barcodeHasher) hash(seq []byte) [16]byte {
	if !h.canonical {
		return md5.Sum(seq)
	}
	h.scratch = h.scratch[:0]
	for _, c := range seq {
		switch {
		case c == '-' || c == ' ' || c == '\t' || c == '\r' || c == '\n':
			continue
		case c >= 'a' && c <= 'z':
			h.scratch = append(h.scratch, c-32)
		default:
			h.scratch = append(h.scratch, c)
		}
	}
	return md5.Sum(h.scratch)
}

func assignUnits(seqBucket map[[16]byte]string, units []barcodeUnit, targets []splitTarget) {
	idx := 0
	for _, t := range targets {
		if idx >= len(units) {
			return
		}
		if t.target < 0 {
			for idx < len(units) {
				seqBucket[units[idx].hash] = t.bucket
				idx++
			}
			return
		}
		acc := 0
		for idx < len(units) && acc < t.target {
			seqBucket[units[idx].hash] = t.bucket
			acc += units[idx].count
			idx++
		}
	}
}

func speciesSet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

func guaranteeTrainingUnit(seqBucket map[[16]byte]string, units []barcodeUnit) {
	for _, unit := range units {
		if seqBucket[unit.hash] == bucketSeenTrain {
			return
		}
	}
	for i := len(units) - 1; i >= 0; i-- {
		if seqBucket[units[i].hash] == bucketSeenVal {
			seqBucket[units[i].hash] = bucketSeenTrain
			return
		}
	}
	for i := len(units) - 1; i >= 0; i-- {
		if seqBucket[units[i].hash] == bucketSeenTest {
			seqBucket[units[i].hash] = bucketSeenTrain
			return
		}
	}
}

func lessHash(a, b [16]byte) bool {
	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

func classHashByte(label string) byte {
	sum := md5.Sum([]byte(label))
	return sum[0]
}

func ceilDiv(a, b int) int {
	if b <= 0 || a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
