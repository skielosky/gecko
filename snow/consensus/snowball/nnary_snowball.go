// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
)

// nnarySnowball is a naive implementation of a multi-color snowball instance
type nnarySnowball struct {
	// preference is the choice with the largest number of successful polls.
	// Ties are broken by switching choice lazily
	preference ids.ID

	// maxSuccessfulPolls maximum number of successful polls this instance has
	// gotten for any choice
	maxSuccessfulPolls int

	// numSuccessfulPolls tracks the total number of successful network polls of
	// the choices
	numSuccessfulPolls map[[32]byte]int

	// snowflake wraps the n-nary snowflake logic
	snowflake nnarySnowflake
}

// Initialize implements the NnarySnowball interface
func (sb *nnarySnowball) Initialize(betaVirtuous, betaRogue int, choice ids.ID) {
	sb.preference = choice
	sb.numSuccessfulPolls = make(map[[32]byte]int)
	sb.snowflake.Initialize(betaVirtuous, betaRogue, choice)
}

// Add implements the NnarySnowball interface
func (sb *nnarySnowball) Add(choice ids.ID) { sb.snowflake.Add(choice) }

// Preference implements the NnarySnowball interface
func (sb *nnarySnowball) Preference() ids.ID {
	// It is possible, with low probability, that the snowflake preference is
	// not equal to the snowball preference when snowflake finalizes. However,
	// this case is handled for completion. Therefore, if snowflake is
	// finalized, then our finalized snowflake choice should be preferred.
	if sb.Finalized() {
		return sb.snowflake.Preference()
	}
	return sb.preference
}

// RecordSuccessfulPoll implements the NnarySnowball interface
func (sb *nnarySnowball) RecordSuccessfulPoll(choice ids.ID) {
	if sb.Finalized() {
		return
	}

	key := choice.Key()
	numSuccessfulPolls := sb.numSuccessfulPolls[key] + 1
	sb.numSuccessfulPolls[key] = numSuccessfulPolls

	if numSuccessfulPolls > sb.maxSuccessfulPolls {
		sb.preference = choice
		sb.maxSuccessfulPolls = numSuccessfulPolls
	}

	sb.snowflake.RecordSuccessfulPoll(choice)
}

// RecordUnsuccessfulPoll implements the NnarySnowball interface
func (sb *nnarySnowball) RecordUnsuccessfulPoll() { sb.snowflake.RecordUnsuccessfulPoll() }

// Finalized implements the NnarySnowball interface
func (sb *nnarySnowball) Finalized() bool { return sb.snowflake.Finalized() }

func (sb *nnarySnowball) String() string {
	return fmt.Sprintf("SB(Preference = %s, NumSuccessfulPolls = %d, SF = %s)",
		sb.preference, sb.maxSuccessfulPolls, &sb.snowflake)
}
