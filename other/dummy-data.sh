#!/usr/bin/env bash
# dummy-data.sh — populate a fresh dredge vault with demo content
#
# Usage:
#   dredge init ./demo-vault
#   bash other/dummy-data.sh [password]

set -e

if [ -n "$1" ]; then
  PASS="$1"
else
  read -rsp "vault password: " PASS
  echo ""
fi
D="dredge --password $PASS"
ASSETS_DIR="$(dirname "$0")/assets"

echo "bootstrapping demo vault..."
echo ""

# ── game ──────────────────────────────────────────────────────────────────────

$D add "My Fish List" -t dredge game fishing list -c $'Snowy Rockling\nBarreleye\nOarfish\nMidnight Shark\nFangtooth\nAnglerfish\nAbyssal Dragonfish\nPale Morwong\nViperfish\nGoblin Shark'

$D add "Aberrant Fish Log" -t dredge lore horror fishing -c $'Night 1: Something brushed the hull. Too large for a regular catch.\nNight 3: Saw it again. Three eyes. Wrong shape entirely.\nNight 7: The nets came up full. I did not want to look.\nNight 12: The Collector was pleased. He did not say why.'

$D add "Boat Upgrade Priority" -t dredge game notes -c $'1. Hull reinforcement (tier II)\n2. Trawl net upgrade\n3. Light range improvement\n4. Engine speed boost\n5. Hold capacity expansion\n-- do NOT upgrade before fixing the hull'

$D add "Dredge Endings" -t dredge game notes -c $'[SPOILERS]\nEnding A: give all relics to the Collector — he opens the rift\nEnding B: destroy the package — burn it at sea\nEnding C: keep it, sail away\nBest ending: debatable. Ending B hits different.'

# ── rankings ──────────────────────────────────────────────────────────────────

$D add "Lovecraft Monster Tierlist" -t horror ranking silly -c $'S: Cthulhu, Azathoth, Yog-Sothoth\nA: Shoggoths, Deep Ones, Elder Things\nB: Mi-Go, Byakhee, Night-Gaunts\nC: Ghouls (too relatable)\nD: Colour Out of Space (not even a creature wtf)\nF: Rats in the Walls (just rats)'

$D add "Album Tierlist" -t music ranking -c $'S: OK Computer, Loveless, In Rainbows\nA: The Downward Spiral, Selected Ambient Works Vol. II\nB: Kid A, Funeral, Dummy\nC: Random Access Memories (fight me)\nNever again: most things'

# ── media ─────────────────────────────────────────────────────────────────────

$D add "Watch List" -t movies tv backlog list -c $'[ ] Annihilation\n[ ] The Lighthouse\n[ ] Leviathan (1989)\n[ ] Deep Rising\n[ ] Below (2002)\n[ ] The Abyss\n[x] Underwater — pretty good actually'

$D add "Horror Watch List" -t horror movies list backlog -c $'[ ] The Thing (1982)\n[ ] Hereditary\n[ ] Midsommar\n[ ] Lake Mungo\n[ ] The Wailing\n[ ] Pontypool\n[x] Annihilation — counts'

$D add "Reading List" -t books reading backlog list -c $'Blood Meridian — Cormac McCarthy\nRoadside Picnic — Strugatsky Brothers\nHouse of Leaves — Mark Z. Danielewski\nThe Terror — Dan Simmons\nAnnihilation — Jeff VanderMeer\nPiranesi — Susanna Clarke'

# ── ssh ───────────────────────────────────────────────────────────────────────

$D add "ssh passphrase hint" -t dev ssh credential password -c $'first pet name + birth year + !\nexample pattern: fluffy1987!\n(no the actual password is not this)'

$D add "ssh config" -t dev ssh config -c $'Host homelab\n  HostName 192.168.1.42\n  User luar\n  IdentityFile ~/.ssh/id_ed25519\n\nHost vps\n  HostName vps.example.com\n  User deploy\n  Port 2222\n  IdentityFile ~/.ssh/id_ed25519'

$D add "ssh hosts" -t dev ssh list -c $'homelab    192.168.1.42   — home server, always on\nvps        vps.example.com — prod, handle with care\nstaging    staging.example.com — break things here\npi         192.168.1.99   — raspberry pi, usually off'

# ── credentials ───────────────────────────────────────────────────────────────

$D add "Grandma's Wifi" -t home nanny password -c $'Network: TIM-Casa-34821\nPassword: gattino2009\n(do not tell grandpa he will change it again)'

$D add "wifi passwords" -t home password list -c $'TIM-Casa-34821     gattino2009\nOffice-Guest       coffee&wifi2024\nvan Houten 2.4G    cantseeme99\nHackerspace        askatthedoor'

$D add "Netflix Password" -t streaming password credential -c $'email: demo@example.com\npassword: Netfl1x!2024\nnote: sharing with 3 people, do not touch the profiles'

$D add "app passwords" -t credential password list -c $'Notion:     demo@example.com / N0tion!pw\nFigma:      demo@example.com / F1gma#2024\nVercel:     demo@example.com / V3rcel$pw\nLinear:     SSO via GitHub'

$D add "GitHub Token" -t dev git credential -c $'ghp_demo1a2b3c4d5e6f7g8h9i0jKLMNOPQRSTUV\nscopes: repo, workflow, read:org\nexpires: 2025-12-31'

$D add "OpenAI API Key" -t dev ai credential -c $'sk-demo1a2b3c4d5e6f7g8h9i0jklmnopqrstuvwxyz1234\norg: personal\nnote: do not commit this. again.'

$D add "Postgres Dev URL" -t dev db credential -c $'postgresql://devuser:devpass@localhost:5432/myapp_dev\nread replica: postgresql://readonly:ropass@localhost:5433/myapp_dev'

# ── personal chaos ────────────────────────────────────────────────────────────

$D add "Ramen Recipe" -t food recipe -c $'broth: pork bones 4h + soy + mirin + kombu\ntare: shio or shoyu depending on mood\nnoodles: thin, slightly alkaline (baked soda trick)\ntoppings: chashu, soft egg (6.5min), nori, menma, scallions\nsecret: MSG is not optional. it is mandatory.'

$D add "coffee ratio" -t food recipe -c $'aeropress:   15g coffee / 200ml water / 80c / 2min\npour over:   18g coffee / 300ml water / 93c / 3min bloom\ncold brew:   80g coffee / 1L water / 24h fridge\nespresso:    18g in / 36g out / 25-30sec'

$D add "grocery list" -t food list personal -c $'-- fridge\n[ ] eggs\n[ ] oat milk\n[ ] miso paste\n[ ] ginger\n-- pantry\n[ ] soy sauce (low sodium)\n[ ] dried ramen noodles\n[ ] MSG (always)\n-- snacks\n[ ] whatever looks good'

$D add "dev notes" -t dev notes personal -c $'- bun is faster than node for scripts, use it\n- check XDG_RUNTIME_DIR before assuming /tmp\n- argon2id params: time=1 mem=64MB threads=4\n- never trust os.TempDir() on linux\n- urfave/cli v2 not v3 (api is different)'

$D add "things i keep forgetting" -t personal notes -c $'- renew domains in november\n- dentist every 6 months (yes still)\n- water the cactus (yes it needs water)\n- call mom on sundays\n- the backup hard drive exists and is under the desk\n- tea timer is 3 minutes not 10'

$D add "Packing List" -t travel personal list -c $'- passport + photocopies\n- all chargers (yes all of them)\n- headphones (both)\n- snacks for airport\n- download stuff offline before flight\n- tell the bank youre traveling\n- empty water bottle for security'

$D add "url stash" -t links bookmarks -c $'https://archive.org/details/lovecraftworks — full Lovecraft archive\nhttps://neal.fun — good time waster\nhttps://poolsuite.net — aesthetic vibes\nhttps://theuselessweb.com — chaos\nhttps://radio.garden — listen to radio anywhere\nhttps://www.youtube.com/watch?v=dQw4w9WgXcQ — very important'

# ── binaries ──────────────────────────────────────────────────────────────────

MYSTERY_PATH="$ASSETS_DIR/experience-tranquility.jpg"
if [ -f "$MYSTERY_PATH" ]; then
  $D add "random qr code" -t random mystery --file "$MYSTERY_PATH"
else
  echo "⚠  mystery image not found at $MYSTERY_PATH"
fi

FEESH_PATH="$ASSETS_DIR/fish/dredge-perch.webp"
if [ -f "$FEESH_PATH" ]; then
  $D add "feesh pictur" -t fishing random --file "$FEESH_PATH"
else
  echo "⚠  feesh not found at $FEESH_PATH"
fi

echo ""
echo "done. run: dredge ls"
