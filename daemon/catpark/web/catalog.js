// The cat's world in one place: the page draws from it and the daemon reads the
// same file (it strips the assignment) to choose what each postcard shows.
// Every id here must have its pictures in a/ and its words in both languages;
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
    "zh": {
      "places": {"workshop": "小工坊", "harbour": "码头", "studio": "画室", "library": "图书馆", "garden": "菜园", "observatory": "天文台", "market": "集市", "kitchen": "厨房", "postoffice": "邮局", "station": "海边小站", "night": "台灯下", "trail": "山路上", "lighthouse": "灯塔下", "hotspring": "雪山温泉", "flowers": "花海", "moonlake": "月光湖边"},
      "acts": {
        "tinker": ["在{p}修一只小钟。", "{p}的小螺丝好难拧。"],
        "fish": ["在{p}钓鱼，浮漂一动不动。", "{p}的鱼今天很害羞。", "在{p}钓到一条小鱼！"],
        "paint": ["在{p}画画，颜料沾到尾巴了。", "把{p}画下来寄给你。"],
        "paper": ["在{p}读报纸，戴上了小眼镜。", "{p}今天的报纸好厚。"],
        "garden": ["在{p}给小苗浇水。", "{p}的花开了一朵。"],
        "stargaze": ["在{p}数星星。", "{p}的天空好大。"],
        "cook": ["在{p}煮了一锅汤。", "{p}飘着好香的味道。"],
        "letters": ["在{p}给你写信。", "{p}的邮票我挑了最好看的。"],
        "music": ["在{p}弹琴唱歌。", "{p}有人跟着我的歌拍手。"],
        "camp": ["在{p}搭了个小帐篷。", "{p}的夜里好安静。"],
        "read": ["在{p}看书，看入迷了。", "{p}找到一本好厚的书。"],
        "tea": ["在{p}喝茶，暖暖的。", "{p}的茶冒着热气。"],
        "eat": ["在{p}吃点心。", "{p}的点心真好吃。"],
        "write": ["在{p}写日记。", "{p}写了满满一页。"],
        "yarn": ["在{p}玩毛线。", "{p}的毛线滚远了。"]
      },
      "friends": {"frog": "一只小青蛙陪着我。", "snail": "一只蜗牛慢慢爬过来看。", "rabbit": "一只兔子坐在旁边。", "owl": "猫头鹰在旁边眨眼。", "hedgehog": "小刺猬也来凑热闹。", "bird": "小鸟落在我身边。"},
      "weathers": {"cloudy": "云厚厚的，很凉快。", "rain": "下雨了，滴滴答答。", "wind": "风好大，胡子都吹歪了。", "snow": "下雪了！"},
      "times": {"dawn": "天刚亮。", "dusk": "太阳快落山了。", "night": "夜深了。"},
      "treat": "带的{t}好好吃。",
      "foods": {"fish": "小鱼干", "berry": "浆果", "catnip": "猫薄荷"},
      "hobbies": {"tinker": "修东西", "fish": "钓鱼", "paint": "画画", "paper": "读报", "garden": "种花", "stargaze": "看星星", "cook": "做饭", "letters": "写信", "music": "弹琴", "camp": "露营"},
      "welcome": "你来啦！这是我的家。",
      "letter": ["今天也想你。", "谢谢你的{t}，很好吃。", "我学会了{h}！", "窗外有只小鸟，想给你看。"]
    },
    "en": {
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
      "treat": "The {t} you packed was lovely.",
      "foods": {"fish": "fish", "berry": "berries", "catnip": "catnip"},
      "hobbies": {"tinker": "mending", "fish": "fishing", "paint": "painting", "paper": "the paper", "garden": "gardening", "stargaze": "stargazing", "cook": "cooking", "letters": "letters", "music": "music", "camp": "camping"},
      "welcome": "You're here! This is my home.",
      "letter": ["Missed you today.", "Thank you for the {t}. Yum.", "I learned {h}!", "A bird came to the window. I wanted you to see."]
    }
  }
};
