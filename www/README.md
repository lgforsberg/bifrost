# www — the Bifrost site

A one-page static site for Bifrost. No build step, no framework, no package
manager. Three files and two images.

Live at **https://bifrost-mail.com**.

```
www/
├── index.html      the whole page
├── css/style.css   the whole stylesheet
├── deploy.sh       publish over ssh
└── img/            bifrost.svg (favicon + mark), bifrost.png (og:image)
```

## Preview

```bash
python3 -m http.server 8000 --directory www
# then open http://localhost:8000
```

Opening `index.html` from disk works too. The only network dependency is Google
Fonts, and the page falls back to a system stack without it.

## Deploy

`deploy.sh` stages the folder over ssh, then swaps it into the docroot in one
local move, so the site is never half-written while a slow upload runs. It
excludes this README and itself.

```bash
export BIFROST_WWW_HOST=user@host        # required
export BIFROST_WWW_PATH=/var/www/…       # required, absolute
export BIFROST_WWW_KEY=~/.ssh/id_rsa     # optional

./www/deploy.sh --dry-run                # show what would change
./www/deploy.sh                          # publish
```

**Dry-run first.** The sync uses `--delete`, so anything sitting in the docroot
that is not in `www/` is removed.

The target is deliberately not committed. This repository is bound for public,
and a hostname baked into a script is an infrastructure disclosure that nobody
remembers to strip on the day the repo flips. The real values for
bifrost-mail.com, along with which `.env` supplies the ssh identity, live in
personal-hq at `context/bifrost-hosting.md`.

The server is plain static nginx with certbot handling TLS, so publishing needs
no reload. Any host that serves a directory works, since every path in the page
is relative and the folder is equally happy at a domain root or in a subpath.
For GitHub Pages, point the Pages source here via an action or copy the contents
to a `gh-pages` branch.

It is deliberately not in `docs/`, which holds `ARCHITECTURE.md` and is written
for readers rather than for a web server.

## Editing

The design lives in one `:root` block at the top of `css/style.css`. The palette
is taken from `assets/bifrost.svg` so the page and the app icon agree: a deep
slate field with one muted arc running cool blue to sage to sand to clay. That
arc is the only saturated thing on the page and it appears exactly twice, in the
hero and on the bridge rule above the demo. Keep it that way.

Typography is DM Sans for anything a person reads and Geist Mono for anything
the machine said. DM Sans was chosen for its near-circular bowls and soft joins,
to offset a page that is otherwise wall-to-wall terminal output, so resist
swapping in a squarer grotesque. The hero headline runs looser tracking than the
other headings for the same reason.

The hero is set as an RFC 822 message, which is the artefact Bifrost actually
composes. Its headers carry the facts a landing page would normally put in an
eyebrow and a row of badges, and the gap before the subject is the blank line
the spec puts between headers and body. If you add something to that block,
make it a true header with a true value.

**The content is the contract.** Every command, flag, error code, exit code and
config path on the page is copied from the README or the source, and the demo
panes reproduce the real table format from `internal/output/output.go` and the
real envelope JSON from `mail/types.go`. When the CLI surface changes, this page
is wrong until someone fixes it. It claims `v1.25.1` in the hero and lists ten
error codes; both are worth a glance at release time.
