// The cat's world in one place: the page draws from it and the daemon reads the
// same file (it strips the assignment) to choose what each postcard shows.
// Every id here must have its pictures in a/ and its words;
// web_test.go checks that.
window.CAT_CATALOG = {
  "levels": [0, 6, 16, 30, 48, 70, 96, 126, 160, 200],
  "foods": ["fish", "berry", "catnip"],
  "hobbies": ["tinker", "fish", "paint", "paper", "garden", "stargaze", "cook", "letters", "music", "camp"],
  "kinds": {
    "workshop": "tinker", "harbour": "fish", "studio": "paint", "library": "paper", "garden": "garden",
    "observatory": "stargaze", "market": "cook", "kitchen": "cook", "postoffice": "letters",
    "station": "music", "night": "letters", "trail": "camp"
  },
  "places": {
    "workshop": {"indoor": true, "spot": [0.385, 0.69, 0.34], "acts": ["tinker", "tea", "read"]},
    "harbour": {"spot": [0.22, 0.5, 0.17], "acts": ["fish", "eat", "music"]},
    "studio": {"indoor": true, "spot": [0.28, 0.77, 0.36], "acts": ["paint", "tea", "write"]},
    "library": {"indoor": true, "spot": [0.34, 0.64, 0.27], "acts": ["paper", "read", "write"]},
    "garden": {"spot": [0.47, 0.79, 0.23], "acts": ["garden", "eat", "yarn"]},
    "observatory": {"indoor": true, "hour": "night", "spot": [0.28, 0.65, 0.25], "acts": ["stargaze", "tea", "write"]},
    "market": {"spot": [0.24, 0.6, 0.22], "acts": ["cook", "eat", "music"]},
    "kitchen": {"indoor": true, "spot": [0.33, 0.6, 0.33], "acts": ["cook", "eat", "tea"]},
    "postoffice": {"indoor": true, "spot": [0.37, 0.68, 0.35], "acts": ["letters", "write", "tea"]},
    "station": {"spot": [0.245, 0.7, 0.2], "acts": ["music", "read", "eat", "paper"]},
    "night": {"indoor": true, "hour": "night", "spot": [0.35, 0.74, 0.37], "acts": ["letters", "write", "tea", "music"]},
    "trail": {"spot": [0.32, 0.7, 0.17], "acts": ["eat", "music", "paint", "read"]},
    "lighthouse": {"hour": "dusk", "spot": [0.42, 0.8, 0.22], "acts": ["paint", "music", "eat", "write"]},
    "hotspring": {"spot": [0.46, 0.75, 0.2], "acts": ["tea", "read", "eat"]},
    "flowers": {"spot": [0.44, 0.82, 0.22], "acts": ["paint", "garden", "music", "eat"]},
    "moonlake": {"hour": "night", "spot": [0.4, 0.84, 0.22], "acts": ["stargaze", "fish", "music", "tea"]}
  },
  "rare": ["lighthouse", "hotspring", "flowers", "moonlake"],
  "friends": ["frog", "snail", "rabbit", "owl", "hedgehog", "bird"],
  "weathers": ["clear", "clear", "cloudy", "rain", "wind", "snow"],
  "words": {
    "places": {"workshop": "the workshop", "harbour": "the harbour", "studio": "the studio", "library": "the library", "garden": "the garden", "observatory": "the observatory", "market": "the market", "kitchen": "the kitchen", "postoffice": "the post office", "station": "the seaside station", "night": "the lamp", "trail": "the trail", "lighthouse": "the lighthouse", "hotspring": "the snowy hot spring", "flowers": "the flower hills", "moonlake": "the moonlit lake"},
    "acts": {
      "tinker": ["Mending a little clock at {p}.", "The screws at {p} are so tiny."],
      "fish": ["Fishing at {p}. The float won't move.", "The fish at {p} are shy today.", "Caught a little fish at {p}!"],
      "paint": ["Painting at {p}. Paint on my tail.", "I painted {p} for you."],
      "paper": ["Reading the paper at {p}, glasses on.", "The paper at {p} is so thick today."],
      "garden": ["Watering the sprouts at {p}.", "A flower opened at {p}."],
      "stargaze": ["Counting stars at {p}.", "The sky at {p} is so big."],
      "cook": ["Made a pot of soup at {p}.", "Something smells good at {p}."],
      "letters": ["Writing to you at {p}.", "I picked the prettiest stamp at {p}."],
      "music": ["Singing with my ukulele at {p}.", "Someone at {p} clapped along."],
      "camp": ["Pitched a little tent on {p}.", "The night on {p} is so quiet."],
      "read": ["Lost in a book at {p}.", "Found a really thick book at {p}."],
      "tea": ["Warm tea at {p}.", "The tea at {p} is still steaming."],
      "eat": ["A snack at {p}.", "The snacks at {p} are so good."],
      "write": ["Writing my diary at {p}.", "Filled a whole page at {p}."],
      "yarn": ["Playing with yarn at {p}.", "My yarn rolled away at {p}."]
    },
    "friends": {"frog": "A little frog kept me company.", "snail": "A snail came slowly to see.", "rabbit": "A rabbit sat beside me.", "owl": "An owl blinked at me.", "hedgehog": "A hedgehog joined in.", "bird": "A bird landed next to me."},
    "weathers": {"cloudy": "Thick clouds, nice and cool.", "rain": "It's raining, pitter-patter.", "wind": "So windy my whiskers bent.", "snow": "It's snowing!"},
    "times": {"dawn": "The sun is just up.", "dusk": "The sun is going down.", "night": "It's late now."},
    "treat": "I loved the {t} you packed.",
    "foods": {"fish": "fish", "berry": "berries", "catnip": "catnip"},
    "hobbies": {"tinker": "mending", "fish": "fishing", "paint": "painting", "paper": "the paper", "garden": "gardening", "stargaze": "stargazing", "cook": "cooking", "letters": "letters", "music": "music", "camp": "camping"},
    "welcome": "You're here! This is my home.",
    "away": {"note": "At {p}", "in": "Back in {n} min", "soon": "Back soon", "line": "Out at {p}. Back in about {n} minutes.", "line_soon": "Out at {p}. Back any minute now."}, "name": {"ask": "What's my name?", "hint": "Name", "ok": "That's me", "sign": "— {n}"},
    "letter": ["Missed you today.", "Thank you for the {t}. Yum.", "I learned {h}!", "A bird came to the window. I wanted you to see."]
  }
};
