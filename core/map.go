package core

import (
	"container/heap"
	"sort"
	"strconv"
)

// MaxMapRoutes bounds the directed routes of one world map.
const MaxMapRoutes = 32768

// MaxRouteTicks bounds one route's duration, so the longest possible path,
// at most one stop per location, cannot overflow int64.
const MaxRouteTicks = 1_000_000_000

// WorldMap is a host world's travel topology and the party's travel state:
// directed routes with durations in the Clock unit, the party that travels
// together, the places it has visited, the places it stops at on the way,
// and the routes that are closed.
type WorldMap struct {
	Routes  []MapRoute    `json:"routes"`
	Party   []CharacterID `json:"party,omitempty"`
	Visited []LocationID  `json:"visited,omitempty"`
	Hazards []LocationID  `json:"hazards,omitempty"`
	Closed  []MapLink     `json:"closed,omitempty"`
}
type MapRoute struct {
	From  LocationID `json:"from"`
	To    LocationID `json:"to"`
	Ticks int64      `json:"ticks"`
}
type MapLink struct {
	From LocationID `json:"from"`
	To   LocationID `json:"to"`
}

// Trip is what one party travel did: the places reached after From, in
// order, the last being where the party now is; the ticks that passed; whether
// a hazard stopped it before the destination; and the places it visited for
// the first time.
type Trip struct {
	From    LocationID   `json:"from"`
	Stops   []LocationID `json:"stops"`
	Ticks   int64        `json:"ticks"`
	Halted  bool         `json:"halted,omitempty"`
	Visited []LocationID `json:"visited,omitempty"`
}

func mapFault(subject, field, expected, actual string) error {
	return fail("E_MAP", subject, field, expected, actual, "invalid map "+field)
}

func cloneWorldMap(m *WorldMap) *WorldMap {
	if m == nil {
		return nil
	}
	return &WorldMap{
		Routes:  append([]MapRoute{}, m.Routes...),
		Party:   append([]CharacterID(nil), m.Party...),
		Visited: append([]LocationID(nil), m.Visited...),
		Hazards: append([]LocationID(nil), m.Hazards...),
		Closed:  append([]MapLink(nil), m.Closed...),
	}
}

func normalizeMap(m *WorldMap) {
	if m == nil {
		return
	}
	sort.Slice(m.Routes, func(i, j int) bool {
		if m.Routes[i].From != m.Routes[j].From {
			return m.Routes[i].From < m.Routes[j].From
		}
		return m.Routes[i].To < m.Routes[j].To
	})
	sort.Slice(m.Party, func(i, j int) bool { return m.Party[i] < m.Party[j] })
	sort.Slice(m.Visited, func(i, j int) bool { return m.Visited[i] < m.Visited[j] })
	sort.Slice(m.Hazards, func(i, j int) bool { return m.Hazards[i] < m.Hazards[j] })
	sort.Slice(m.Closed, func(i, j int) bool {
		if m.Closed[i].From != m.Closed[j].From {
			return m.Closed[i].From < m.Closed[j].From
		}
		return m.Closed[i].To < m.Closed[j].To
	})
}

// validateMap checks a world map against its world. It needs a Clock, for
// route durations, and excludes the battle and adventure travel models.
func validateMap(b *Blueprint, refs Refs) error {
	m := b.Map
	if m == nil {
		return nil
	}
	if b.Clock == nil {
		return mapFault("", "clock", "declared clock", "absent")
	}
	if b.Battle != nil || b.Adventure != nil {
		return mapFault("", "travel model", "map without battle or adventure", "both")
	}
	if len(m.Routes) > MaxMapRoutes {
		return mapFault("", "routes", "at most 32768 routes", "exceeded")
	}
	location := func(id LocationID) error { return refs.CheckRef(Ref{LocationKind, string(id)}) }
	routes := make(map[MapLink]bool, len(m.Routes))
	for _, r := range m.Routes {
		if err := location(r.From); err != nil {
			return err
		}
		if err := location(r.To); err != nil {
			return err
		}
		if r.From == r.To {
			return mapFault(string(r.From), "route", "different endpoints", string(r.To))
		}
		if r.Ticks < 1 || r.Ticks > MaxRouteTicks {
			return mapFault(string(r.From), "route ticks", "1..1000000000", strconv.FormatInt(r.Ticks, 10))
		}
		link := MapLink{r.From, r.To}
		if routes[link] {
			return mapFault(string(r.From), "route", "unique direction", "duplicate")
		}
		routes[link] = true
	}
	party := map[CharacterID]bool{}
	for _, id := range m.Party {
		if err := refs.CheckRef(Ref{CharacterKind, string(id)}); err != nil {
			return err
		}
		if party[id] {
			return mapFault(string(id), "party", "unique member", "duplicate")
		}
		party[id] = true
	}
	for _, list := range []struct {
		field string
		ids   []LocationID
	}{{"visited", m.Visited}, {"hazard", m.Hazards}} {
		seen := map[LocationID]bool{}
		for _, id := range list.ids {
			if err := location(id); err != nil {
				return err
			}
			if seen[id] {
				return mapFault(string(id), list.field, "unique location", "duplicate")
			}
			seen[id] = true
		}
	}
	closed := map[MapLink]bool{}
	for _, l := range m.Closed {
		if !routes[l] {
			return mapFault(string(l.From), "closed", "declared route", string(l.From)+" -> "+string(l.To))
		}
		if closed[l] {
			return mapFault(string(l.From), "closed", "unique route", "duplicate")
		}
		closed[l] = true
	}
	return nil
}

// mapGraph is a map's open routes from each location, with the visited and
// hazard sets, built for one search.
type mapGraph struct {
	out     map[LocationID][]MapRoute
	visited map[LocationID]bool
	hazard  map[LocationID]bool
}

// graph builds the routes travel may use, with hazard as the places a trip
// stops at. Visited places, and the place the search starts from, may be
// passed through. openAll also uses closed routes and passAll lets every
// place be passed through, so that travelRefusal can name why a strict
// search failed.
func (m *WorldMap) graph(start LocationID, hazard map[LocationID]bool, openAll, passAll bool) mapGraph {
	g := mapGraph{out: map[LocationID][]MapRoute{}, visited: map[LocationID]bool{start: true}, hazard: hazard}
	closed := map[MapLink]bool{}
	if !openAll {
		for _, l := range m.Closed {
			closed[l] = true
		}
	}
	for _, r := range m.Routes {
		if !closed[MapLink{r.From, r.To}] {
			g.out[r.From] = append(g.out[r.From], r)
		}
	}
	for _, id := range m.Visited {
		g.visited[id] = true
	}
	if passAll {
		for _, r := range m.Routes {
			g.visited[r.From], g.visited[r.To] = true, true
		}
	}
	return g
}

// mapSearch is the quickest path from one start to every place reachable by
// the travel rule: every stop on the way is a place that may be passed
// through, the last one may be any place. Ties go to fewer stops, then to the
// path whose stops, compared in order, have the smaller IDs.
type mapSearch struct {
	start LocationID
	ticks map[LocationID]int64
	hops  map[LocationID]int
	pred  map[LocationID]LocationID
}

type mapEntry struct {
	ticks int64
	hops  int
	at    LocationID
}
type mapQueue []mapEntry

func (q mapQueue) Len() int { return len(q) }
func (q mapQueue) Less(i, j int) bool {
	if q[i].ticks != q[j].ticks {
		return q[i].ticks < q[j].ticks
	}
	if q[i].hops != q[j].hops {
		return q[i].hops < q[j].hops
	}
	return q[i].at < q[j].at
}
func (q mapQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *mapQueue) Push(x any)   { *q = append(*q, x.(mapEntry)) }
func (q *mapQueue) Pop() any {
	old := *q
	x := old[len(old)-1]
	*q = old[:len(old)-1]
	return x
}

// path lists the stops from the start to at, the start excluded.
func (s *mapSearch) path(at LocationID) []LocationID {
	var out []LocationID
	for at != s.start {
		out = append(out, at)
		at = s.pred[at]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// lessPath reports whether the path to a sorts before the path to z; both are
// settled and have the same number of stops. Walking back in step, the paths
// agree from the place they meet back to the start, so the last pair that
// differs is their first difference.
func (s *mapSearch) lessPath(a, z LocationID) bool {
	less := false
	for a != z {
		less = a < z
		a, z = s.pred[a], s.pred[z]
	}
	return less
}

func (g mapGraph) search(start LocationID) *mapSearch {
	s := &mapSearch{start: start, ticks: map[LocationID]int64{start: 0}, hops: map[LocationID]int{start: 0}, pred: map[LocationID]LocationID{}}
	done := map[LocationID]bool{}
	q := &mapQueue{{0, 0, start}}
	for q.Len() > 0 {
		// A place's first entry out of the queue is its best; later ones
		// are stale.
		e := heap.Pop(q).(mapEntry)
		if done[e.at] {
			continue
		}
		done[e.at] = true
		if !g.visited[e.at] {
			continue
		}
		for _, r := range g.out[e.at] {
			t, h := e.ticks+r.Ticks, e.hops+1
			old, seen := s.ticks[r.To]
			better := !seen || t < old || t == old && h < s.hops[r.To] ||
				t == old && h == s.hops[r.To] && s.lessPath(e.at, s.pred[r.To])
			if better {
				s.ticks[r.To], s.hops[r.To], s.pred[r.To] = t, h, e.at
				heap.Push(q, mapEntry{t, h, r.To})
			}
		}
	}
	return s
}

// leg is where a trip toward to ends: the first hazard on the way, or to.
func (g mapGraph) leg(s *mapSearch, to LocationID) (stops []LocationID, ticks int64, halted bool) {
	stops = s.path(to)
	for i, at := range stops[:len(stops)-1] {
		if g.hazard[at] {
			stops = stops[:i+1]
			halted = true
			break
		}
	}
	return stops, s.ticks[stops[len(stops)-1]], halted
}

// travelRefusal names why no path reaches to: no route at all, a closed
// route, a stop the party has not visited, or both.
func (m *WorldMap) travelRefusal(from, to LocationID) string {
	reach := func(openAll, passAll bool) bool {
		_, ok := m.graph(from, nil, openAll, passAll).search(from).ticks[to]
		return ok
	}
	switch {
	case !reach(true, true):
		return "no route"
	case reach(false, true):
		return "unvisited stop"
	case reach(true, false):
		return "closed route"
	}
	return "closed route and unvisited stop"
}

// applyMapTravel moves the party along the quickest open path to its
// destination, or to the first hazard on the way, passes the time travelled
// and records the places visited. Any failure rejects the whole request.
func applyMapTravel(b *Blueprint, intent PartyTravel, ctx applicationContext) ([]Event, error) {
	m := b.Map
	actor := CharacterID(ctx.cause.Actor.ID)
	if !containsID(m.Party, actor) {
		return nil, mapFault(ctx.cause.Actor.ID, "travel actor", "party member", ctx.cause.Actor.ID)
	}
	from := character(b, actor).Location
	for _, id := range m.Party {
		if c := character(b, id); c.Location != from {
			return nil, mapFault(string(id), "travel party", "party together at "+string(from), string(c.Location))
		}
	}
	if intent.To == from {
		return nil, mapFault(string(intent.To), "travel", "different destination", string(from))
	}
	g := m.graph(from, b.hazards(), false, false)
	s := g.search(from)
	if _, ok := s.ticks[intent.To]; !ok {
		return nil, mapFault(string(intent.To), "travel path", "open path through visited places", m.travelRefusal(from, intent.To))
	}
	stops, ticks, halted := g.leg(s, intent.To)
	end := stops[len(stops)-1]
	events := []Event{}
	apply := func(op Operation) error {
		if ctx.work != nil {
			*ctx.work++
			if *ctx.work > MaxOperations {
				return abilityFault("E_LIMIT", "work", "at most 128", "travel exceeded")
			}
		}
		es, err := applyOperation(b, op, ctx)
		if err == nil {
			events = append(events, es...)
		}
		return err
	}
	for _, id := range m.Party {
		if err := apply(Operation{Move: &Move{Character: id, To: end}}); err != nil {
			return nil, err
		}
	}
	if err := apply(Operation{AdvanceTime: &AdvanceTime{Ticks: ticks}}); err != nil {
		return nil, err
	}
	trip := Trip{From: from, Stops: stops, Ticks: ticks, Halted: halted}
	for _, at := range []LocationID{from, end} {
		if !containsID(m.Visited, at) {
			m.Visited = append(m.Visited, at)
			trip.Visited = append(trip.Visited, at)
		}
	}
	return append(events, Event{Kind: PartyTraveled, Travel: copyPtr(&intent), Trip: &trip}), nil
}

func containsID[T comparable](list []T, x T) bool {
	for _, v := range list {
		if v == x {
			return true
		}
	}
	return false
}

// MapProjection is where a Character is on the world map: the routes out of
// its location and every place the party travel rule reaches from there,
// each with its ticks, the stops on the way and the hazard that would stop
// the party first, if any.
type MapProjection struct {
	Revision     Revision         `json:"revision"`
	Clock        *Clock           `json:"clock,omitempty"`
	Character    CharacterSummary `json:"character"`
	Location     Location         `json:"location"`
	Exits        []MapExit        `json:"exits"`
	Destinations []MapDestination `json:"destinations"`
}
type MapExit struct {
	To      Location `json:"to"`
	Ticks   int64    `json:"ticks"`
	Visited bool     `json:"visited"`
	Hazard  bool     `json:"hazard"`
	Closed  bool     `json:"closed"`
}
type MapDestination struct {
	To      Location    `json:"to"`
	Ticks   int64       `json:"ticks"`
	Stops   int         `json:"stops"`
	Visited bool        `json:"visited"`
	HaltsAt *LocationID `json:"halts_at,omitempty"`
}

func (w *World) MapView(id CharacterID) (MapProjection, error) {
	s := w.Snapshot()
	b := &s.Blueprint
	if err := b.CheckRef(Ref{CharacterKind, string(id)}); err != nil {
		return MapProjection{}, err
	}
	if b.Map == nil {
		return MapProjection{}, mapFault(string(id), "map", "world map", "absent")
	}
	m := b.Map
	c := character(b, id)
	names := make(map[LocationID]Location, len(b.Locations))
	for _, l := range b.Locations {
		if _, ok := names[l.ID]; !ok {
			names[l.ID] = l
		}
	}
	p := MapProjection{Revision: s.Revision, Clock: copyPtr(b.Clock), Character: CharacterSummary{c.ID, c.Name}, Location: names[c.Location], Exits: []MapExit{}, Destinations: []MapDestination{}}
	visited := map[LocationID]bool{}
	for _, at := range m.Visited {
		visited[at] = true
	}
	closed := map[MapLink]bool{}
	for _, l := range m.Closed {
		closed[l] = true
	}
	g := m.graph(c.Location, b.hazards(), false, false)
	for _, r := range m.Routes {
		if r.From == c.Location {
			p.Exits = append(p.Exits, MapExit{To: names[r.To], Ticks: r.Ticks, Visited: visited[r.To], Hazard: g.hazard[r.To], Closed: closed[MapLink{r.From, r.To}]})
		}
	}
	search := g.search(c.Location)
	// A place's first hazard is its predecessor's, or the predecessor itself;
	// places in order of stops see their predecessor's first.
	order := keys(search.ticks)
	sort.SliceStable(order, func(i, j int) bool { return search.hops[order[i]] < search.hops[order[j]] })
	halts := map[LocationID]LocationID{}
	for _, to := range order {
		if to == c.Location {
			continue
		}
		d := MapDestination{To: names[to], Ticks: search.ticks[to], Stops: search.hops[to], Visited: visited[to]}
		if at := search.pred[to]; at != c.Location {
			if h, ok := halts[at]; ok {
				halts[to] = h
			} else if g.hazard[at] {
				halts[to] = at
			}
		}
		if h, ok := halts[to]; ok {
			d.HaltsAt = copyPtr(&h)
		}
		p.Destinations = append(p.Destinations, d)
	}
	sort.Slice(p.Destinations, func(i, j int) bool { return p.Destinations[i].To.ID < p.Destinations[j].To.ID })
	return p, nil
}

const (
	PartyJoined   EventKind = "PartyJoined"
	PartyLeft     EventKind = "PartyLeft"
	RouteClosed   EventKind = "RouteClosed"
	RouteOpened   EventKind = "RouteOpened"
	HazardMarked  EventKind = "HazardMarked"
	HazardCleared EventKind = "HazardCleared"
	VisitMarked   EventKind = "VisitMarked"
)

// MapEdit changes the map's party, closed routes, declared hazards or
// visited places in play.
// Exactly one variant is present. The routes themselves change only with the
// world.
type MapEdit struct {
	JoinParty   *CharacterID `json:"join_party,omitempty"`
	LeaveParty  *CharacterID `json:"leave_party,omitempty"`
	CloseRoute  *MapLink     `json:"close_route,omitempty"`
	OpenRoute   *MapLink     `json:"open_route,omitempty"`
	MarkHazard  *LocationID  `json:"mark_hazard,omitempty"`
	ClearHazard *LocationID  `json:"clear_hazard,omitempty"`
	MarkVisited *LocationID  `json:"mark_visited,omitempty"`
}

func cloneMapEdit(e *MapEdit) *MapEdit {
	if e == nil {
		return nil
	}
	return &MapEdit{JoinParty: copyPtr(e.JoinParty), LeaveParty: copyPtr(e.LeaveParty), CloseRoute: copyPtr(e.CloseRoute), OpenRoute: copyPtr(e.OpenRoute), MarkHazard: copyPtr(e.MarkHazard), ClearHazard: copyPtr(e.ClearHazard), MarkVisited: copyPtr(e.MarkVisited)}
}

func (e MapEdit) variants() int {
	n := 0
	for _, yes := range []bool{e.JoinParty != nil, e.LeaveParty != nil, e.CloseRoute != nil, e.OpenRoute != nil, e.MarkHazard != nil, e.ClearHazard != nil, e.MarkVisited != nil} {
		if yes {
			n++
		}
	}
	return n
}

// checkMapEdit checks what a map edit names: a map, the character, a
// declared route, the location.
func (b Blueprint) checkMapEdit(e MapEdit) error {
	if e.variants() != 1 {
		return fail("E_IR", "", "map edit", "exactly one map change", "invalid sum", "map edit requires exactly one variant")
	}
	if b.Map == nil {
		return mapFault("", "map", "world map", "absent")
	}
	switch {
	case e.JoinParty != nil || e.LeaveParty != nil:
		id := e.JoinParty
		if id == nil {
			id = e.LeaveParty
		}
		return b.CheckRef(Ref{CharacterKind, string(*id)})
	case e.CloseRoute != nil || e.OpenRoute != nil:
		l := e.CloseRoute
		if l == nil {
			l = e.OpenRoute
		}
		for _, r := range b.Map.Routes {
			if r.From == l.From && r.To == l.To {
				return nil
			}
		}
		return mapFault(string(l.From), "route", "declared route", string(l.From)+" -> "+string(l.To))
	}
	at := e.MarkHazard
	if at == nil {
		at = e.ClearHazard
	}
	if at == nil {
		at = e.MarkVisited
	}
	return b.CheckRef(Ref{LocationKind, string(*at)})
}

// applyMapEdit makes one map change. A change that is already so is no
// change. Clearing a place that is a hazard only because a hostile occupant
// is there names the occupant: resolving it clears the place.
func applyMapEdit(b *Blueprint, e MapEdit) ([]Event, error) {
	m := b.Map
	var kind EventKind
	var subject string
	changed := true
	switch {
	case e.JoinParty != nil:
		subject, kind = string(*e.JoinParty), PartyJoined
		m.Party, changed = with(m.Party, *e.JoinParty)
	case e.LeaveParty != nil:
		subject, kind = string(*e.LeaveParty), PartyLeft
		m.Party, changed = without(m.Party, *e.LeaveParty)
	case e.CloseRoute != nil:
		subject, kind = string(e.CloseRoute.From), RouteClosed
		m.Closed, changed = with(m.Closed, *e.CloseRoute)
	case e.OpenRoute != nil:
		subject, kind = string(e.OpenRoute.From), RouteOpened
		m.Closed, changed = without(m.Closed, *e.OpenRoute)
	case e.MarkHazard != nil:
		subject, kind = string(*e.MarkHazard), HazardMarked
		m.Hazards, changed = with(m.Hazards, *e.MarkHazard)
	case e.ClearHazard != nil:
		at := *e.ClearHazard
		subject, kind = string(at), HazardCleared
		m.Hazards, changed = without(m.Hazards, at)
		for _, o := range b.Occupants {
			if !changed && o.Location == at && o.Attitude == AttitudeHostile && present(o) {
				return nil, mapFault(subject, "hazard", "declared hazard", "hostile occupant "+string(o.ID))
			}
		}
	case e.MarkVisited != nil:
		subject, kind = string(*e.MarkVisited), VisitMarked
		m.Visited, changed = with(m.Visited, *e.MarkVisited)
	}
	if !changed {
		return nil, noChange(subject)
	}
	normalizeMap(m)
	return []Event{{Kind: kind, MapEdit: cloneMapEdit(&e)}}, nil
}

// with adds x to list unless it is there already.
func with[T comparable](list []T, x T) ([]T, bool) {
	if containsID(list, x) {
		return list, false
	}
	return append(list, x), true
}

// without removes x from list, if it is there.
func without[T comparable](list []T, x T) ([]T, bool) {
	for i, v := range list {
		if v == x {
			return append(list[:i:i], list[i+1:]...), true
		}
	}
	return list, false
}

func mapEditEvent(k EventKind) bool {
	switch k {
	case PartyJoined, PartyLeft, RouteClosed, RouteOpened, HazardMarked, HazardCleared, VisitMarked:
		return true
	}
	return false
}
