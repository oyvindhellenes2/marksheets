# Døme-sider

Ei lita, oppdikta side-samling som viser kva Marksheets gjer. `pages/` er ignorert av dette
repoet — sidene mine ligg for seg sjølv — så dette er innhaldet du kan sjå på utan å ha dine
eigne sider.

Køyr appen mot dei direkte:

```sh
PAGES_DIR=examples /opt/homebrew/bin/go run ./cmd/marksheets
```

eller kopier dei inn som eit utgangspunkt:

```sh
mkdir -p pages && cp examples/*.json pages/
```

## Kva dei viser

| Fil | Viser |
|---|---|
| `kafeen.json` | overskrifter som nestar, oppgåver med arbeidsside, `Arkiv`, data-linjer, ein tabell, lister med underpunkt, emneknaggar, og `@`-spørjingar av alle tre slag |
| `menyen.json` | sida det blir henta frå: data-linjer med eining, og `#vegetar`-merkte punkt |
| `byggje-ny-disk.json` | ei **arbeidsside** — ho høyrer til oppgåva «Byggje ny disk» på `kafeen`, og er berre nåbar gjennom henne |

`@menyen/kaffi/espresso` hentar eitt tal inn midt i ei setning, `@menyen/kaffi` hentar heile
bolken, og `@menyen/mat[#vegetar]` hentar alt som er merkt — rekna ut på nytt kvar gong sida
blir vist.
