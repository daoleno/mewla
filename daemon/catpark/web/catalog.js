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
    "harbour": {"spot": [0.22, 0.5, 0.17], "friend": "left", "acts": ["fish", "eat", "music", "view", "stroll"]},
    "studio": {"indoor": true, "spot": [0.28, 0.77, 0.36], "acts": ["paint", "tea", "write"]},
    "library": {"indoor": true, "spot": [0.34, 0.64, 0.27], "acts": ["paper", "read", "write"]},
    "garden": {"spot": [0.47, 0.79, 0.23], "acts": ["garden", "eat", "yarn", "view", "stroll"]},
    "observatory": {"indoor": true, "hour": "night", "spot": [0.28, 0.65, 0.25], "acts": ["stargaze", "tea", "write"]},
    "market": {"spot": [0.24, 0.6, 0.22], "acts": ["cook", "eat", "music", "stroll"]},
    "kitchen": {"indoor": true, "spot": [0.33, 0.6, 0.33], "acts": ["cook", "eat", "tea"]},
    "postoffice": {"indoor": true, "spot": [0.37, 0.68, 0.35], "acts": ["letters", "write", "tea"]},
    "station": {"spot": [0.245, 0.7, 0.2], "acts": ["music", "read", "eat", "paper", "view", "stroll"]},
    "night": {"indoor": true, "hour": "night", "spot": [0.35, 0.74, 0.37], "acts": ["letters", "write", "tea", "music"]},
    "trail": {"spot": [0.32, 0.7, 0.17], "acts": ["eat", "music", "paint", "read", "view", "stroll"]},
    "lighthouse": {"hour": "dusk", "spot": [0.42, 0.8, 0.22], "acts": ["paint", "music", "eat", "write", "view"]},
    "hotspring": {"spot": [0.46, 0.75, 0.2], "acts": ["tea", "read", "eat", "view"]},
    "flowers": {"spot": [0.44, 0.82, 0.22], "acts": ["paint", "garden", "music", "eat", "view", "stroll"]},
    "moonlake": {"hour": "night", "spot": [0.4, 0.84, 0.22], "acts": ["stargaze", "fish", "music", "tea", "view"]}
  },
  "rare": ["lighthouse", "hotspring", "flowers", "moonlake"],
  "friends": ["frog", "snail", "rabbit", "owl", "hedgehog", "bird"],
  "likes": {"frog": "fish", "owl": "fish", "bird": "berry", "hedgehog": "berry", "rabbit": "catnip", "snail": "catnip"},
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
      "yarn": ["Playing with yarn at {p}.", "My yarn rolled away at {p}."],
      "view": ["Just looking out at {p}.", "Sat at {p} a long time, just looking."],
      "stroll": ["Off for a walk at {p}, bundle and all.", "You nearly missed me at {p}!"]
    },
    "friends": {"frog": "A little frog kept me company.", "snail": "A snail came slowly to see.", "rabbit": "A rabbit sat beside me.", "owl": "An owl blinked at me.", "hedgehog": "A hedgehog joined in.", "bird": "A bird landed next to me."},
    "weathers": {"cloudy": "Thick clouds, nice and cool.", "rain": "It's raining, pitter-patter.", "wind": "So windy my whiskers bent.", "snow": "It's snowing!"},
    "times": {"dawn": "The sun is just up.", "dusk": "The sun is going down.", "night": "It's late now."},
    "treat": "I loved the {t} you packed.",
    "foods": {"fish": "fish", "berry": "berries", "catnip": "catnip"},
    "hobbies": {"tinker": "mending", "fish": "fishing", "paint": "painting", "paper": "the paper", "garden": "gardening", "stargaze": "stargazing", "cook": "cooking", "letters": "letters", "music": "music", "camp": "camping"},
    "welcome": "You're here! This is my home.",
    "away": {"note": "At {p}", "in": "Back in {n} min", "soon": "Back soon", "line": "Out at {p}. Back in about {n} minutes.", "line_soon": "Out at {p}. Back any minute now."}, "name": {"ask": "What's my name?", "hint": "Name", "ok": "That's me", "sign": "— {n}"},
    "diary": {
      "title": "While you were away",
      "quiet": "Nothing much. I was right here.",
      "days": "{n} days since you came by.",
      "slots": {
        "dawn": ["Had tea by the window.", "Watched the garden wake up over tea."],
        "morning": ["Wrote at my desk all morning.", "Wrote you a letter. Then another one."],
        "noon": ["Had a big lunch.", "Lunch! Fish soup, my favourite."],
        "afternoon": ["Napped in the afternoon sun.", "A long nap after lunch."],
        "dusk": ["Cooked a little soup.", "Made supper at the stove."],
        "evening": ["Read by the lamp.", "Read until the lamp got warm."],
        "night": ["Slept like a loaf.", "Slept all night, curled up tight."]
      },
      "hobby": "Spent the afternoon {h}.",
      "out": "Went to {p}.",
      "back": "Back by {t}.",
      "learnt": "Took up {h}!",
      "brought": {"palette": "Brought home a palette. It's by the tea table.", "bottle": "Brought home a bottle. It's on the table.", "book": "Brought home a book. It's by the rug.", "wrench": "Brought home a wrench. It's by the table.", "lantern": "Brought home a lantern. It's by the bed.", "pot": "Brought home a little pot. It's on the floor.", "telescope": "Brought home a telescope. It's by the desk.", "bell": "Brought home a bell. It's on the peg.", "teapot": "Brought home a teapot. It's on the stove.", "stamp": "Brought home a stamp. It's on my desk.", "compass": "Brought home a compass. It's on the floor.", "pinecone": "Brought home a pinecone. It's by the table."},
      "friend": {"frog": "A frog dropped by. It left you a fish.", "owl": "An owl came by in the night and left a fish.", "bird": "A bird tapped on the window and left a berry.", "hedgehog": "A hedgehog came round. It left some berries.", "rabbit": "A rabbit hopped in and left some catnip.", "snail": "A snail came to visit, slowly. It left some catnip."},
      "grew": {"frame": "Framed one of my postcards for the wall.", "paint": "Painted a picture and hung it up.", "cushion": "Made a cushion, for when friends come.", "bunting": "Hung up little flags. It feels like a party."}
    },
    "letter": ["Missed you today.", "Thank you for the {t}. Yum.", "I learned {h}!", "A bird came to the window. I wanted you to see."]
  }
};
