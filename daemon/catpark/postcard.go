package catpark

import (
	"math/rand/v2"
	"time"
)

// compose chooses what a trip postcard shows: the place the Work sends the cat
// to (now and then a rare one, for a cat you are close to), what it is doing
// there, the hour it was sent, the day's weather, a friend met on the way and
// the snack it took. It never repeats a scene already in the album, so two
// Works never send the same card, and neither do two cards of one Work.
func (s *State) compose(trip *Trip, phase string, now time.Time, world *Catalog) Postcard {
	id := postcardID(trip.WorkID, phase)
	seed := hashOf(id)
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)<<7|1))
	level := s.Care.Level(world)
	card := Postcard{
		ID: id, WorkID: trip.WorkID, Kind: trip.Kind, Phase: phase,
		Place: string(trip.Kind), Time: timeOfDay(now), Weather: weatherOf(now, world),
		Treat: trip.Packed, Seed: seed, At: now.UnixMilli(),
	}
	if _, ok := world.Places[card.Place]; !ok {
		card.Place = string(KindTrail)
	}
	if phase == PhaseLong && level >= 4 && len(world.Rare) > 0 && rng.Float64() < 0.2+0.05*float64(level-4) {
		card.Place = world.Rare[rng.IntN(len(world.Rare))]
	}
	if hour := world.Places[card.Place].Hour; hour != "" {
		card.Time = hour
	}
	taught := world.HobbyOf(trip.Kind)
	seen := map[string]bool{}
	for _, other := range s.Postcards {
		seen[other.scene()] = true
	}
	for attempt := 0; attempt < 32; attempt++ {
		card.Act = s.pickAct(world.Places[card.Place], taught, world, rng)
		card.Friend = pickFriend(level, card.Time, world, rng, attempt)
		if !seen[card.scene()] {
			break
		}
		if attempt >= 12 && len(world.Weathers) > 0 {
			// a crowded album: let the weather vary too
			card.Weather = world.Weathers[rng.IntN(len(world.Weathers))]
		}
	}
	return card
}

// scene is what makes two postcards look alike.
func (p Postcard) scene() string {
	return p.Place + "|" + p.Act + "|" + p.Time + "|" + p.Weather + "|" + p.Friend
}

// pickAct favours the hobby the place teaches, then hobbies the cat has, then
// the everyday things it does anywhere. A hobby it has not met is never shown.
func (s *State) pickAct(place Place, taught string, world *Catalog, rng *rand.Rand) string {
	total := 0
	weights := make([]int, len(place.Acts))
	for index, act := range place.Acts {
		switch {
		case act == taught:
			weights[index] = 3
		case world.isHobby(act) && s.Care.knows(act):
			weights[index] = 2
		case world.isHobby(act):
			weights[index] = 0
		default:
			weights[index] = 1
		}
		total += weights[index]
	}
	if total == 0 {
		return "read"
	}
	roll := rng.IntN(total)
	for index, weight := range weights {
		if roll < weight {
			return place.Acts[index]
		}
		roll -= weight
	}
	return place.Acts[0]
}

// pickFriend: friends join from level 4, more often the closer you are.
// An owl only comes at night and a bird only by day.
func pickFriend(level int, hour string, world *Catalog, rng *rand.Rand, attempt int) string {
	chance := 0.15 * float64(level-3)
	if attempt > 4 {
		chance += 0.3 // a crowded album wants company to tell cards apart
	}
	if level < 4 || len(world.Friends) == 0 || rng.Float64() >= chance {
		return ""
	}
	for tries := 0; tries < 8; tries++ {
		friend := world.Friends[rng.IntN(len(world.Friends))]
		if friend == "owl" && hour != "night" || friend == "bird" && hour == "night" {
			continue
		}
		return friend
	}
	return ""
}

func timeOfDay(now time.Time) string {
	switch hour := now.Hour(); {
	case hour >= 5 && hour < 8:
		return "dawn"
	case hour >= 8 && hour < 17:
		return "day"
	case hour >= 17 && hour < 20:
		return "dusk"
	}
	return "night"
}

// weatherOf is one weather for the whole day, so a day's cards agree. Snow
// only falls in the cold months.
func weatherOf(now time.Time, world *Catalog) string {
	if len(world.Weathers) == 0 {
		return ""
	}
	weather := world.Weathers[variantOf("weather/"+now.Format("2006-01-02"), len(world.Weathers))]
	if weather == "snow" {
		if month := now.Month(); month != time.December && month != time.January && month != time.February {
			return "cloudy"
		}
	}
	return weather
}

func (s *State) addPostcard(trip *Trip, phase string, now time.Time, world *Catalog) {
	if s.hasPostcard(postcardID(trip.WorkID, phase)) {
		return
	}
	s.Postcards = append(s.Postcards, s.compose(trip, phase, now, world))
	s.LastPostcardAt = now.UnixMilli()
}

// letter is a note the cat writes at home once it has taken up letters: about
// its favourite food or the newest thing it learnt.
func (s *State) letter(now time.Time) bool {
	id := "pc:letter:" + now.Format("2006-01-02")
	if s.hasPostcard(id) {
		return false
	}
	card := Postcard{ID: id, Phase: PhaseLetter, Treat: s.Care.Favorite(), Seed: hashOf(id), At: now.UnixMilli()}
	if n := len(s.Care.Hobbies); n > 0 {
		card.Hobby = s.Care.Hobbies[n-1]
	}
	s.Postcards = append(s.Postcards, card)
	return true
}
