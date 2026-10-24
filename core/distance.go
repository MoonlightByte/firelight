package core

// Coordinates and square size are bounded by checkBattle, so this exact
// Chebyshev distance fits int64. Weapon reach and authored range use one metric.
func gridDistance(a, z BattlePosition, squareSize int64) int64 {
	dx, dy := a.X-z.X, a.Y-z.Y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dy > dx {
		dx = dy
	}
	return dx * squareSize
}

func (b *Blueprint) characterDistance(a, z CharacterID) (int64, bool) {
	if a == z {
		return 0, true
	}
	e := b.activeBattle()
	if e == nil || character(b, a).Location != e.Location || character(b, z).Location != e.Location {
		return 0, false
	}
	x, y := battleParticipant(e, a), battleParticipant(e, z)
	if x == nil || y == nil {
		return 0, false
	}
	return gridDistance(x.Admission.Position, y.Admission.Position, b.Battle.Rules.SquareSize), true
}

// A known exact distance always takes precedence. Otherwise an explicitly
// authored enclosing span can prove range without pretending it is distance.
func (b *Blueprint) withinRange(a, z CharacterID, maximum int64) bool {
	if distance, known := b.characterDistance(a, z); known {
		return distance <= maximum
	}
	location := character(b, a).Location
	if location != character(b, z).Location {
		return false
	}
	span, known := b.locationSpan(location)
	return known && span <= maximum
}

func (b *Blueprint) locationSpan(location LocationID) (int64, bool) {
	if b.Battle != nil {
		for _, span := range b.Battle.Rules.LocationSpans {
			if span.Location == location {
				return span.Maximum, true
			}
		}
	}
	return 0, false
}

func (b *Blueprint) checkLocationSpans() error {
	spans := b.Battle.Rules.LocationSpans
	if len(spans) > 128 {
		return battleFault("location spans", "at most 128", "exceeded")
	}
	seen := map[LocationID]bool{}
	for _, span := range spans {
		if err := b.CheckRef(Ref{Kind: LocationKind, ID: string(span.Location)}); err != nil {
			return err
		}
		if span.Maximum < 1 || seen[span.Location] {
			return battleFault("location span", "unique Location with positive maximum separation", string(span.Location))
		}
		seen[span.Location] = true
	}
	return nil
}

// All positions have already passed the existing bounded-grid validation.
func (b *Blueprint) checkFormationSpan(location LocationID, positions map[BattlePosition]bool) error {
	span, known := b.locationSpan(location)
	if !known {
		return nil
	}
	var low, high BattlePosition
	first := true
	for p := range positions {
		if first {
			low, high, first = p, p, false
		}
		low.X, low.Y = min(low.X, p.X), min(low.Y, p.Y)
		high.X, high.Y = max(high.X, p.X), max(high.Y, p.Y)
	}
	if gridDistance(low, high, b.Battle.Rules.SquareSize) > span {
		return battleFault("formation span", "within declared Location maximum separation", string(location))
	}
	return nil
}
