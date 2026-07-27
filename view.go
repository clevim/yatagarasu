package main

import "html/template"

// favicon is the 512px assets/icon.png reduced to 64px and inlined: the pages
// are served from a scratch image with no static file route, and the key gate
// renders before any ?key= exists, so it cannot fetch one behind auth either.
const favicon = `
<link rel="icon" href="data:image/png;base64,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">`

// baseHead is everything the three pages share inside <head>.
//
// Yatagarasu is the sun crow, so the shelf is ink with one warm ember accent.
// The page is read on a phone next to the e-reader, or on a desktop tab left
// open for a moment — one family, fixed scale, no decoration that is not data.
const baseHead = favicon + `
<style>
 :root{
   --bg:oklch(0.97 0.006 55); --surface:oklch(1 0.002 55); --raise:oklch(0.99 0.004 55);
   --line:oklch(0.90 0.008 55); --ink:oklch(0.24 0.020 50); --muted:oklch(0.48 0.018 50);
   --accent:oklch(0.52 0.170 40); --accent-solid:oklch(0.50 0.170 38); --on-accent:oklch(0.99 0.01 55);
   --ok:oklch(0.44 0.100 155); --ok-bg:oklch(0.93 0.040 155);
   --radius:.5rem; --ease:cubic-bezier(.22,1,.36,1);
 }
 @media (prefers-color-scheme:dark){
   :root{
     --bg:oklch(0.18 0.012 50); --surface:oklch(0.22 0.014 50); --raise:oklch(0.25 0.014 50);
     --line:oklch(0.31 0.014 50); --ink:oklch(0.93 0.008 60); --muted:oklch(0.73 0.012 60);
     --accent:oklch(0.78 0.140 55); --accent-solid:oklch(0.72 0.150 50); --on-accent:oklch(0.20 0.030 50);
     --ok:oklch(0.78 0.110 160); --ok-bg:oklch(0.30 0.050 160);
   }
 }
 *{box-sizing:border-box}
 html{color-scheme:light dark}
 body{
   margin:0;background:var(--bg);color:var(--ink);
   font:15px/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;
   -webkit-text-size-adjust:100%;
 }
 h1,h2,h3{margin:0;font-weight:620;letter-spacing:-.01em;text-wrap:balance}
 h1{font-size:1.5rem}
 h2{font-size:1.0625rem}
 a{color:var(--accent)}
 :focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:3px}
 .btn{
   display:inline-flex;align-items:center;gap:.4rem;border:1px solid var(--line);
   background:var(--surface);color:var(--ink);border-radius:var(--radius);
   padding:.5rem .85rem;font:inherit;font-weight:550;text-decoration:none;cursor:pointer;
   transition:background 160ms var(--ease),border-color 160ms var(--ease),transform 160ms var(--ease);
 }
 .btn:hover{background:var(--raise);border-color:var(--muted)}
 .btn:active{transform:translateY(1px)}
 .btn.primary{background:var(--accent-solid);border-color:transparent;color:var(--on-accent)}
 .btn.primary:hover{filter:brightness(1.07)}
 .btn.ghost{border-color:transparent;background:none;color:var(--muted);padding:.25rem .5rem;font-size:.8125rem;font-weight:500}
 .btn.ghost:hover{background:var(--raise);border-color:var(--line);color:var(--ink)}
 .field{display:block;margin-bottom:.9rem}
 .field span{display:block;font-size:.8125rem;color:var(--muted);margin-bottom:.3rem}
 .field input{
   width:100%;padding:.55rem .7rem;font:inherit;color:var(--ink);
   background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);
 }
 .field input:focus{outline:2px solid var(--accent);outline-offset:1px;border-color:transparent}
 @media (prefers-reduced-motion:reduce){*{transition-duration:1ms!important;animation-duration:1ms!important}}
</style>`

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — shelf</title>` + baseHead + `
<style>
 .wrap{max-width:60rem;margin:0 auto;padding:2rem 1.25rem 4rem}
 header.top{display:flex;flex-wrap:wrap;gap:1rem;align-items:center;justify-content:space-between;margin-bottom:1.75rem}
 header.top p{margin:.15rem 0 0;color:var(--muted);font-size:.875rem}
 header.top .actions{display:flex;gap:.5rem;flex-wrap:wrap}
 .crow{color:var(--accent)}

 /* Stat strip: hairline-separated figures, not a row of identical cards.
    The dividers only exist where the row cannot wrap; a border stranded at the
    start of a wrapped line reads as a mistake. */
 .stats{display:grid;grid-template-columns:repeat(2,1fr);gap:1rem 1.5rem;padding:1.1rem 1.25rem;margin-bottom:1.5rem;
        background:var(--surface);border:1px solid var(--line);border-radius:var(--radius)}
 .stat b{display:block;font-size:1.5rem;font-weight:600;letter-spacing:-.02em;font-variant-numeric:tabular-nums;line-height:1.2}
 .stat span{font-size:.8125rem;color:var(--muted)}
 @media (min-width:46rem){
   .stats{grid-template-columns:repeat(5,1fr);gap:0}
   .stat+.stat{border-left:1px solid var(--line);padding-left:1.5rem}
 }

 .cols{display:grid;grid-template-columns:1fr;gap:1.5rem;margin-bottom:2rem;align-items:start}
 @media (min-width:52rem){.cols{grid-template-columns:1.6fr 1fr}}
 .panel{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:1.1rem 1.25rem}
 .panel h2{margin-bottom:.9rem}
 .panel .hint{color:var(--muted);font-size:.8125rem;margin:0}

 /* The chart is an SVG viewBox, so bar geometry comes from the server and
    nothing depends on how a flex container decides to share out its width. */
 .chart{display:block;width:100%;height:6rem;margin-bottom:.5rem}
 .chart .track{fill:var(--line);opacity:.5}
 .chart .bar{fill:var(--accent-solid)}
 .chart .bar.today{fill:var(--accent)}
 .axis{display:flex;justify-content:space-between;margin:0 0 .7rem;color:var(--muted);font-size:.75rem}
 .axis b{color:var(--ink);font-weight:600}

 .recent{list-style:none;margin:0;padding:0}
 .recent li{display:flex;justify-content:space-between;gap:1rem;padding:.45rem 0;border-bottom:1px solid var(--line)}
 .recent li:last-child{border-bottom:0}
 .recent b{font-weight:550}
 .recent em{display:block;font-style:normal;color:var(--muted);font-size:.8125rem}
 .recent time{color:var(--muted);font-size:.8125rem;white-space:nowrap}

 .manga{margin-bottom:1.5rem;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);overflow:hidden}
 .manga>header{display:flex;flex-wrap:wrap;gap:.5rem 1rem;align-items:baseline;justify-content:space-between;
               padding:.85rem 1.25rem;border-bottom:1px solid var(--line);background:var(--raise)}
 .manga>header p{margin:0;color:var(--muted);font-size:.8125rem;font-variant-numeric:tabular-nums}
 table{width:100%;border-collapse:collapse;font-size:.9375rem}
 td,th{text-align:left;padding:.55rem 1.25rem;border-bottom:1px solid var(--line)}
 tr:last-child td{border-bottom:0}
 tbody tr{transition:background 120ms var(--ease)}
 tbody tr:hover{background:var(--raise)}
 th{font-size:.75rem;font-weight:550;color:var(--muted);text-transform:none}
 td.num{color:var(--muted);font-variant-numeric:tabular-nums;width:4.5rem}
 td.size,td.when{color:var(--muted);font-size:.8125rem;white-space:nowrap}
 td.state{width:1%;white-space:nowrap;text-align:right}
 .tag{display:inline-block;padding:.1rem .45rem;border-radius:999px;font-size:.75rem;font-weight:550;
      background:var(--ok-bg);color:var(--ok)}
 form.inline{display:inline;margin-left:.35rem}
 @media (max-width:40rem){
   td.size,th.size{display:none}
   td,th{padding-inline:.85rem}
 }

 .empty{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:2rem 1.5rem;max-width:38rem}
 .empty h2{margin-bottom:.5rem}
 .empty p{color:var(--muted);margin:.5rem 0 0}
 .empty ol{color:var(--muted);margin:.75rem 0 0;padding-left:1.1rem}
 .empty li{margin:.3rem 0}
</style>
<body><div class="wrap">

<header class="top">
  <div>
    <h1><span class="crow">八</span> Yatagarasu</h1>
    <p>KOReader shelf for Karasu</p>
  </div>
  <div class="actions">
    <a class="btn" href="/settings{{.KeyQuery}}">Settings</a>
    <a class="btn primary" href="/plugin.zip{{.KeyQuery}}" download>Download KOReader plugin</a>
  </div>
</header>

<div class="stats">
  <div class="stat"><b>{{.Chapters}}</b><span>{{if eq .Chapters 1}}chapter{{else}}chapters{{end}} on the shelf</span></div>
  <div class="stat"><b>{{.MangaN}}</b><span>{{if eq .MangaN 1}}manga{{else}}manga{{end}}</span></div>
  <div class="stat"><b>{{.Unread}}</b><span>waiting to be read</span></div>
  <div class="stat"><b>{{.Finished7}}</b><span>finished this week</span></div>
  <div class="stat"><b>{{.Size}}</b><span>on disk</span></div>
</div>

<div class="cols">
  <section class="panel">
    <h2>Finished chapters</h2>
    {{if .HasStats}}
    <svg class="chart" viewBox="0 0 {{.ChartW}} {{.ChartH}}" preserveAspectRatio="none"
         role="img" aria-label="Chapters finished per day over the last {{.ChartDays}} days">
      {{range .Bars}}<g><title>{{.Title}}</title>
        <rect class="track" x="{{.X}}" y="0" width="{{$.BarW}}" height="{{$.ChartH}}"></rect>
        {{if .Count}}<rect class="bar{{if .Today}} today{{end}}" x="{{.X}}" y="{{.Y}}" width="{{$.BarW}}" height="{{.H}}"></rect>{{end}}
      </g>{{end}}
    </svg>
    <p class="axis"><span>{{.ChartFrom}}</span><b>today</b></p>
    <p class="hint">As reported by the e-reader. {{.Finished7}} finished this week.</p>
    {{else}}
    <p class="hint">No chapters finished yet. Once KOReader reports one back, the last {{.ChartDays}} days show up here.</p>
    {{end}}
  </section>

  <section class="panel">
    <h2>Recently finished</h2>
    {{if .Recent}}
    <ul class="recent">
      {{range .Recent}}<li><b>{{.Chapter}}<em>{{.Manga}}</em></b><time>{{.When}}</time></li>{{end}}
    </ul>
    {{else}}
    <p class="hint">Nothing reported yet. Chapters show up here once KOReader syncs them back.</p>
    {{end}}
  </section>
</div>

{{if .Manga}}
{{range .Manga}}
<section class="manga">
  <header>
    <h2>{{.Title}}</h2>
    <p>{{.TotalN}} {{if eq .TotalN 1}}chapter{{else}}chapters{{end}} · {{.Size}}{{if .ReadN}} · {{.ReadN}} read{{end}}</p>
  </header>
  <table>
    <thead><tr><th>#</th><th>Chapter</th><th class="size">Size</th><th>Updated</th><th></th></tr></thead>
    <tbody>
      {{range .Chapters}}
      <tr>
        <td class="num">{{.Number}}</td>
        <td>{{.Name}}</td>
        <td class="size">{{.Size}}</td>
        <td class="when" title="{{.WhenFull}}">{{.When}}</td>
        <td class="state">
          {{if .Read}}<span class="tag">read</span>
          <form class="inline" method="post" action="/api/shelf/{{.ID}}/read">
            <input type="hidden" name="read" value="false">
            <input type="hidden" name="key" value="{{$.Key}}">
            <button class="btn ghost" type="submit" title="Undo a read report sent by mistake">undo</button>
          </form>{{end}}
          <form class="inline" method="post" action="/api/shelf/{{.ID}}/delete"
                onsubmit="return confirm('Remove {{.Name}} from the shelf? Karasu re-uploads it on its next sync if it still wants it here.')">
            <input type="hidden" name="key" value="{{$.Key}}">
            <button class="btn ghost" type="submit" title="Free the disk now. Does not delete anything already on the e-reader.">remove</button>
          </form>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
</section>
{{end}}
{{else}}
<section class="empty">
  <h2>The shelf is empty</h2>
  <p>Karasu uploads only chapters it has already downloaded <strong>as CBZ</strong>. If a sync ran and nothing arrived, one of these is usually why:</p>
  <ol>
    <li><em>Save chapters as CBZ</em> is off in Karasu's download settings — chapters downloaded as loose images are skipped.</li>
    <li>No categories are selected under <em>Settings → Yatagarasu</em>.</li>
    <li>The chapters are not downloaded yet; Karasu queues them and picks them up on the next run.</li>
  </ol>
  <p>Chapters appear here within a sync of fixing that.</p>
</section>
{{end}}

</div>
`))

var settingsTmpl = template.Must(template.New("settings").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — settings</title>` + baseHead + `
<style>
 .wrap{max-width:38rem;margin:0 auto;padding:2rem 1.25rem 4rem}
 header.top{display:flex;flex-wrap:wrap;gap:1rem;align-items:center;justify-content:space-between;margin-bottom:1.5rem}
 header.top p{margin:.15rem 0 0;color:var(--muted);font-size:.875rem}
 .crow{color:var(--accent)}
 .panel{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);
        padding:1.1rem 1.25rem;margin-bottom:1.25rem}
 .panel h2{margin-bottom:.25rem}
 .panel>p.hint{color:var(--muted);font-size:.8125rem;margin:0 0 1rem}
 .field .note{display:block;margin-top:.3rem;color:var(--muted);font-size:.75rem}
 .field:last-of-type{margin-bottom:0}
 .choices{display:flex;gap:.5rem;flex-wrap:wrap;margin-bottom:.3rem}
 .choices label{display:inline-flex;align-items:center;gap:.35rem;padding:.4rem .7rem;cursor:pointer;
                border:1px solid var(--line);border-radius:var(--radius);background:var(--bg);font-size:.875rem}
 .choices label:has(input:checked){border-color:var(--accent);background:var(--raise);font-weight:550}
 .choices input{accent-color:var(--accent-solid)}
 .save{display:flex;gap:.75rem;align-items:center;flex-wrap:wrap}
 .facts{list-style:none;margin:0;padding:0;font-size:.875rem}
 .facts li{display:flex;justify-content:space-between;gap:1rem;padding:.4rem 0;border-bottom:1px solid var(--line)}
 .facts li:last-child{border-bottom:0}
 .facts span{color:var(--muted)}
 .facts b{font-weight:550;font-variant-numeric:tabular-nums;text-align:right}
 .danger{border-color:oklch(0.62 0.150 25 / .45)}
 .danger .row{display:flex;flex-wrap:wrap;gap:.75rem 1rem;align-items:center;justify-content:space-between;
              padding:.7rem 0;border-bottom:1px solid var(--line)}
 .danger .row:last-child{border-bottom:0;padding-bottom:0}
 .danger .row p{margin:0;font-size:.8125rem;color:var(--muted);max-width:24rem}
 .danger .row strong{display:block;color:var(--ink);font-weight:550;font-size:.9375rem}
 .flash{padding:.7rem 1rem;border-radius:var(--radius);margin-bottom:1.25rem;font-size:.875rem}
 .flash.ok{background:var(--ok-bg);color:var(--ok)}
 .flash.bad{background:oklch(0.93 0.050 25);color:oklch(0.44 0.150 25)}
 @media (prefers-color-scheme:dark){.flash.bad{background:oklch(0.30 0.060 25);color:oklch(0.84 0.110 25)}}
</style>
<body><div class="wrap">

<header class="top">
  <div>
    <h1><span class="crow">八</span> Settings</h1>
    <p>Stored in {{.DataDir}}/settings.json, applied without a restart</p>
  </div>
  <a class="btn" href="/{{.KeyQuery}}">Back to the shelf</a>
</header>

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<form method="post" action="/settings">
  <input type="hidden" name="key" value="{{.Key}}">

  <section class="panel">
    <h2>How clients reach this shelf</h2>
    <p class="hint">Both are baked into the plugin ZIP the moment it is downloaded.</p>
    <label class="field"><span>Public URL</span>
      <input name="publicUrl" value="{{.PublicURL}}" placeholder="http://192.168.0.10:3080"
             inputmode="url" autocapitalize="off" autocorrect="off" spellcheck="false">
      <em class="note">Leave blank to use whatever host the browser asked for. Set it behind a reverse
        proxy, or the plugin gets a base_url that only fails on the e-reader.</em>
    </label>
    <label class="field"><span>API key</span>
      <input name="apiKey" value="{{.APIKey}}" autocomplete="off" autocapitalize="off"
             autocorrect="off" spellcheck="false">
      <em class="note">Blank means unauthenticated requests are accepted — fine on a trusted LAN,
        nowhere else. Karasu and the plugin both need the new value after a change.</em>
    </label>
  </section>

  <section class="panel">
    <h2>Time and history</h2>
    <p class="hint">It is {{.Now}} on this shelf right now.</p>
    <label class="field"><span>Timezone</span>
      <input name="timezone" value="{{.Timezone}}" placeholder="America/Sao_Paulo" list="tz"
             autocapitalize="off" autocorrect="off" spellcheck="false">
      <datalist id="tz">
        {{range .Zones}}<option value="{{.}}">{{end}}
      </datalist>
      <em class="note">An IANA name. The container runs on UTC, which puts the day boundary on the
        activity chart at 21:00 in Brazil and labels every bar a day early.</em>
    </label>
    <div class="field"><span>Activity chart window</span>
      <div class="choices">
        {{$d := .ChartDays}}
        {{range .DayChoices}}
        <label><input type="radio" name="chartDays" value="{{.}}"{{if eq . $d}} checked{{end}}>{{.}} days</label>
        {{end}}
      </div>
    </div>
  </section>

  <div class="save">
    <button class="btn primary" type="submit">Save</button>
    <a class="btn ghost" href="/{{.KeyQuery}}">Cancel</a>
  </div>
</form>

<section class="panel" style="margin-top:1.25rem">
  <h2>What is on disk</h2>
  <p class="hint">Yatagarasu {{.Version}}</p>
  <ul class="facts">
    <li><span>Chapters on the shelf</span><b>{{.Entries}}</b></li>
    <li><span>Recorded read events</span><b>{{.Events}}</b></li>
    <li><span>Data directory</span><b>{{.DataDir}}</b></li>
  </ul>
</section>

<section class="panel danger">
  <h2>Danger zone</h2>
  <p class="hint">Neither of these touches anything already downloaded to the e-reader.</p>
  <div class="row">
    <div>
      <strong>Clear reading history</strong>
      <p>Empties the activity chart and the recently-finished list. Read flags on the chapters stay.</p>
    </div>
    <form method="post" action="/settings" onsubmit="return confirm('Clear {{.Events}} recorded read events?')">
      <input type="hidden" name="key" value="{{.Key}}">
      <input type="hidden" name="action" value="clear-history">
      <button class="btn" type="submit">Clear history</button>
    </form>
  </div>
  <div class="row">
    <div>
      <strong>Empty the shelf</strong>
      <p>Deletes all {{.Entries}} CBZ files and their metadata. Karasu re-uploads whatever it still
        wants here on its next sync, so this frees the disk rather than vetoing anything.</p>
    </div>
    <form method="post" action="/settings" onsubmit="return confirm('Delete all {{.Entries}} chapters from the shelf?')">
      <input type="hidden" name="key" value="{{.Key}}">
      <input type="hidden" name="action" value="empty-shelf">
      <button class="btn" type="submit">Empty shelf</button>
    </form>
  </div>
</section>

</div>
`))
