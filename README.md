# MF-API Output Branch

This branch serves as a **static API** — JSON files generated daily by the
[MF-API](https://github.com/Junior1Gamer/MF-API) scraper on the `main` branch.

All files are served by GitHub Pages at:

```
https://junior1gamer.github.io/MF-API/
```

---

## Base URL

```
https://junior1gamer.github.io/MF-API
```

All paths below are relative to this base. GitHub Pages sets permissive CORS
headers so you can call these endpoints from any origin.

---

## Endpoints

### `GET /metadata.json`

Dataset metadata.

**Response**
```json
{
  "generated_at": "2026-04-26T04:17:00Z",
  "total_manga": 53241,
  "manga_file": "manga.json",
  "detail_prefix": "manga/"
}
```

| Field | Type | Description |
|---|---|---|
| `generated_at` | string (ISO 8601) | When the scrape completed |
| `total_manga` | number | Number of manga entries in the listing |
| `manga_file` | string | Relative path to the full manga list |
| `detail_prefix` | string | Prefix for per-manga detail files |

---

### `GET /manga.json`

Full listing of every manga on MangaFire. This is the entry point for
discovery — lightweight enough to fetch once and cache.

**Response** — Array of:
```json
[
  {
    "slug": "one-piece.1",
    "title": "One Piece",
    "cover": "https://s.mfcdn.nl/.../cover.jpg",
    "url": "https://mangafire.to/manga/one-piece.1"
  }
]
```

| Field | Type | Description |
|---|---|---|
| `slug` | string | Unique identifier used in detail file names |
| `title` | string | Manga title |
| `cover` | string (URL) | Cover image URL from MangaFire's CDN |
| `url` | string (URL) | Link to the manga page on MangaFire's website |

**Size:** ~3–5 MB for the full listing (53K+ entries).

---

### `GET /manga/{slug}.json`

Full metadata for a single manga, including its chapter list.

**Example:** `/manga/one-piece.1.json`

**Response**
```json
{
  "slug": "one-piece.1",
  "title": "One Piece",
  "cover": "https://s.mfcdn.nl/.../cover.jpg",
  "description": "Gol D. Roger was known as the Pirate King...",
  "status": "Releasing",
  "genres": ["Action", "Adventure", "Comedy", "Drama", "Fantasy", "Shounen"],
  "alt_titles": ["ワンピース", "航海王"],
  "chapters": [
    {
      "number": "1",
      "title": "Romance Dawn - The Dawn of the Adventure",
      "date": "Jul 22, 1997",
      "url": "https://mangafire.to/read/one-piece.1/en/chapter-1"
    }
  ],
  "updated_at": "2026-04-25T..."
}
```

| Field | Type | Description |
|---|---|---|
| `slug` | string | Unique identifier |
| `title` | string | Primary title |
| `cover` | string (URL) | Cover image URL (may be empty) |
| `description` | string | Synopsis (may be empty or truncated) |
| `status` | string | `Releasing`, `Completed`, `Discontinued`, etc. (may be empty) |
| `genres` | string[] | Array of genre tags |
| `alt_titles` | string[] | Alternative titles when available |
| `chapters` | Chapter[] | Array of chapter metadata (see below) |
| `updated_at` | string | Last-updated string from the detail page |

**Chapter object:**

| Field | Type | Description |
|---|---|---|
| `number` | string | Chapter number (may be `"1"`, `"10.5"`, etc.) |
| `title` | string | Chapter title (may be empty) |
| `date` | string | Upload/publish date string |
| `url` | string (URL) | Link to the chapter reader on MangaFire |

**Size:** ~2–10 KB per manga depending on chapter count.

---

### `GET /manga/{slug}/chapters/{num}.json`

Page image URLs for a specific chapter. Only exists if a **chapters** scrape
has been run for this manga (see
[limitation note](https://github.com/Junior1Gamer/MF-API#running-locally)).

**Example:** `/manga/one-piece.1/chapters/1.json`

**Response**
```json
{
  "slug": "one-piece.1",
  "chapter": "1",
  "title": "Romance Dawn - The Dawn of the Adventure",
  "pages": [
    { "url": "https://s.mfcdn.nl/.../page-001.jpg", "scrambled_offset": 0 },
    { "url": "https://s.mfcdn.nl/.../page-002.jpg", "scrambled_offset": 0 }
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `slug` | string | Manga identifier |
| `chapter` | string | Chapter number |
| `title` | string | Chapter title |
| `pages` | PageImage[] | Array of page image objects |

**PageImage object:**

| Field | Type | Description |
|---|---|---|
| `url` | string (URL) | Page image URL from MangaFire's CDN |
| `scrambled_offset` | number | **0** = clean image; **>0** = pixels are rearranged (see [Scrambled images](#scrambled-images)) |

---

## Scrambled images

Some chapter page images returned by MangaFire's internal API have their
pixels rearranged in a grid pattern as an anti-scraping measure. When
`scrambled_offset > 0`, each 200×200 px tile of the image is displaced.

**Unscramble formula** (ported from the Kotatsu reader source):

```
for each tile at destination position (x, y):
  srcX = (maxX - x + offset) % maxX
  srcY = (maxY - y + offset) % maxY
  copy tile from (srcX, srcY) to (x, y)
```

Where `maxX = ceil(width / 200)` and `maxY = ceil(height / 200)`.

This is a pure-pixel rearrangement — no encryption, no compression change.
A canvas-based unscrambler in the browser is the recommended approach.

---

## CORS

GitHub Pages serves all files with the following CORS header, allowing
requests from any origin:

```
access-control-allow-origin: *
```

No preflight (`OPTIONS`) is needed for `GET` requests.

---

## Caching

GitHub Pages sets `Cache-Control: max-age=600` (10 minutes) by default.
For production apps, consider:

- **Short-poll `metadata.json`** every 10–30 minutes to detect new data
- **Cache `manga.json`** for 1 hour (it changes at most once per day)
- **Cache `manga/{slug}.json`** indefinitely — it only updates on title
  corrections, not chapter additions (chapters are added as new files, not
  appended)

You can always bypass the cache by appending a cache-busting query string:
```
/manga.json?_=1745645000
```

---

## Errors

The API is static — there are no server-side errors. If a file doesn't exist,
GitHub Pages returns a standard `404` HTML page. This can happen when:

- A manga slug is mistyped
- A chapter pages file hasn't been scraped yet
- The listing is still being generated (during a workflow run)

Check `metadata.json` first to verify the dataset is current.

---

## Usage guidelines

- **Be respectful.** This dataset represents 53K+ manga from a live website.
  Don't fetch the full listing more than once per hour.
- **No hotlinking images.** Cover and page image URLs point to MangaFire's
  CDN. Display them in your app but don't hotlink in a way that drives
  excessive traffic to their origin.
- **Cache aggressively.** The dataset updates once per day at most. There's
  no benefit to polling more frequently.
- **Scrambled images.** If you see garbled tiles, check `scrambled_offset`
  and apply the unscramble formula.

---

## Quick start

```javascript
const BASE = 'https://junior1gamer.github.io/MF-API';

// 1. Get the listing
const list = await fetch(`${BASE}/manga.json`).then(r => r.json());
console.log(`${list.length} manga available`);

// 2. Pick a random manga
const random = list[Math.floor(Math.random() * list.length)];
console.log(random.title, random.slug);

// 3. Fetch its detail
const detail = await fetch(`${BASE}/manga/${random.slug}.json`).then(r => r.json());
console.log(detail.description?.slice(0, 100));
console.log(`${detail.chapters.length} chapters`);

// 4. If chapter pages were scraped, show the first chapter's pages
const ch1 = await fetch(`${BASE}/manga/${random.slug}/chapters/${detail.chapters[0].number}.json`)
  .then(r => r.ok ? r.json() : null);
if (ch1) {
  ch1.pages.forEach(p => {
    const img = document.createElement('img');
    img.src = p.url;
    document.body.appendChild(img);
  });
}
```

---

For the full project source code, architecture, and local development
instructions, see the [`main` branch](https://github.com/Junior1Gamer/MF-API).
