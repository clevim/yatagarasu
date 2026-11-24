package main

import "html/template"

// favicon is the 512px assets/icon.png reduced to 64px and inlined: the pages
// are served from a scratch image with no static file route, and the key gate
// renders before any ?key= exists, so it cannot fetch one behind auth either.
const favicon = `
<link rel="icon" href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAMAAACdt4HsAAAA+VBMVEUSCwgSCwgSCwgSCwcTDAgSCwcSCwgTDAgOCQYqGAxGKRNoOBd4SSKIVSeXWCZqQR5NMRcdEQmrbDHIeDW6dTWqZSy1bTGGTSJwSCLMgjsmFQo6IxFYNBc2Ggs7IQ/5l0L1kT7XhDubYi14RB3qhjjzizpHHQlTHgdYJQtpKgyGLAh5KQinXSeKNAyVPhDlfDLFcC65ZimJRRnXfDV3Mg5qMA+ZRBTZcyx3OBPHZyeoSRWJOxFLIwysQA+1Qw5ULBK1ShOqUhvskEH/oUfHWBm5VBrBThPTXRrWaSXSYR2nPAyZUR+XNgu2XCOQLwjldyvgaSGuPg3HXiEI5PJXAAAAB3RSTlMBFVibzsivHFHIHgAAB/pJREFUWMOll4t2osoShieTmc1N0RYvsaFNCGi4CIoCKhqEbEiIIboz7/8wu8BcTHTmnLNOuRaCWB9/F91V1d++lXb2/fwH9T/Yj/PvZ98O7PvPP/2bpk/9+vP7u/vZ+Uk/huUqVZ7nK7U6g0794fxVxNlfx/cQW2kIQrMB/nyrLQjtDnuC8deecPx85qLZbVyw+NUHYbbW6DYvmGMN5fiP3KvdZkVkOU569SASx4k9gFaPEBCHsy/xQxfdpoQp1L68vJILD5GXry4vrykKS81u7ctAfp59FSA2ujVMYUUCAJggskJ50pIUROGLbkP8KuFzBGryNYEvVlXal1eF1ZSrq37/6uq6oxY3eq2u9CUKh/MHVWXQiAc3rFprgR9YTSm/+nxF7mkDGFpFrhwO48e3gwt8LbAU0Q2TAGAIXqra55TiCABFJgPT0glVF/hDwgEAN5q9gWWbBCF2pNS5zlhVVU6BgzPh2M6UYERc2x2wzUPCBwBdC57l6wwipsnOZs5wwinjAuBw3GTozGY91yWI6LY173ZOASqXi2CJ0Wp5G1qMtGbZNSexPKfwMCOKK4mxo3C5QtgOxpe1Y4Ak86K7pEgQ3QbW2xREmGEQeruyoyCKCfLnLC+zXwFMt0KJrWkPk8LwhwHkzfa3WLklUtUm/gK4biBR4OVF8G4x2OtZcGgLmW+KuFn5BKDrd3xVvRMvpt7Kc73VCVtrxUHXOLnG3snVa1WkDwCo/ffl5d93DMU3VkHiI1iAoL04UOUIbgaDwIekoEXrdpUS74rJfX2ggJbkFqx6maHR9Ti9tzXNT2NCrQJ7MCBBmuq3t9pDolmanslVRItys9VqySz9oaDBw/PWMqhCvPoIA02zzNPch8hP3CCNvCgCgBfZqVqFBFefrkBf6/pDASuzCJG5ygETdfqzNF6MZn7oZ9l4tFgkoCDQHhN9k8oXhd6K6swRkro9+g1QbcB4n/jpGNPlilzk6eODnyz6QqdWaY9m8zwHgLsQipVIE2E6BMVYuHgDIEEhuj6UWIdn4Deabc6yx2wxbVUlCdGI78cFYCY0isDTYqMlSrzne9XG2xDYrmdsLL5GI67SK57B8OpIbbOIZuqFh/qc566qdlDxRLED0AqfJ5bUZfYA+qKp2bf50xBOsbifYZJwOW3zXOmCxs4mHTfr+yUrIppGzsR/8JmuRO8VXPM3eZ7HI+6gljBVFd41VwLa6mhawe/VhaYUtQS0K3sAal4M8txyxkLlIOvStbu7fr30gtzCHRQnEaLN+w+B4bT2QwApAyvfPnNspSPhdwCr9oX9Vaevsu8ApsZXRAUUBPlzcw8Qu2wBSFsQcYw/ysO0X6H381QV3qUhBkOknCUAtrGACgDNypxnB3Ywq0B8DqQq7ddx43btsNLSqJPZQRLY8RTvAWpc2mI05NjDMoqPTgoFLDccLUoHR2bKIbDdXpkv5tNOR5FE/KdKj3qSMunM1qXDWmD2Cro9Cuk6JmPldCfwpVmglSHkZ10nawHvgyjPXS9OCeJmXLn+f+9d1GvEjdcIDcJIk/ZBpBghvg+eFyuCn3bPPM+R3wMI98Q/7/wbEw+iVFPa1OtESkPb3pg5cbPdhClEUie6CVS2OuRply3NzY2WbzSef11MfJYE29zIyTx+cjiozRIcqE/hgAvMreEWt5j4vrkxTVigLWU/lWllBLMiN/PBxMdz5zl2JsUEIIcvrxgWnjhDBx4wic28AOjj+iuAHT0WgK0R+JCYlo8LjjCiMlM+NMxnSo/pcYtsCXVmkhglYDLGbwnF+SewtgCIfO1GN1+CZLGYzdTWhwBnl2WQ2/yNhty5nxhbHQAJ/54TlV1g7AH2MrLCbf7iJ8lI+lBQ293f+7BiUw/Hz35aAjaz9XtOJFlgGKZhBr7vp/69DdnhfsdTb+8CJkcri/JtbiVuAYhMQ9e3qYM+0vrklx/AJ/UNM7cf4Fl2oko0UyYx+MK0oiaAtRJLt/Jl6vu2H2TcQV1gksAF823D3C4BsLn9Z9yTWjUKUiShqjzLTrOgANiGYSzL/6YpPiyuXlqseB2iuwckjji5aiJ8AU0RRaYqx87Sd4BbjDpdH9ZGCvkTqNyebVmgLUkfnR40VxJdB5kVkVaugDBO0vQhsOAfLkNIPPlS3kka5IYHgVjsFk9Pk54yUiFLd1ia5mo0HvdHHAtt8yKDOEeWkUfx1/6AWieR5VoYL+MVoejeLNutaWYI+UscIrqm7qBqodU6hhbHtYz7ZEUf9UheYpsuVLg4XsNyf44yZ1KsFkiAHX68SJ4xYrg4niDkWvbDmj7R5rmpDQAv87w1t443eZRlsyHDtmbZItjeTiQJfv+lASBKvJN9YqEBoVXq+YPUi7dGHka/dnVllKQhXMTzeBV7KYFeK9FON5oUpaVLrN1HgZtEuWFswyiZsdzsPrqFlxekiZdGEcHLaEX9DgA9ie+Gm81tCBPbMF7CZAJBScKNYZqmEQabfAMvinzKFN8+b9awG4X5dls4mMYmWhJ45f4eYG7LUbnok/+Pow3PwI5e3gAhWULTtgpzs7zOX0KLHG16jnY80OyGL3mByF/MkGihu9mW7mFoDajjPc/Z8ZYR2u38ZQNrevMS6gbEpDh7sfUT6Rq2PMcS9gzdKLxKA4KpkZNbx++nt337NIwwGQxubgawyhB1umSd/3bj+VbF9va7OvPX2R+3vv/Rzs/+y833aTvYfP9f2/9/AR/nk+JzhVaLAAAAAElFTkSuQmCC">`

// baseHead is everything every page shares inside <head>.
//
// Yatagarasu is the sun crow: warm paper & ink with one ember accent.
// Zero external fonts, zero icon CDNs, zero decorative chrome that is not data.
const baseHead = favicon + `
<style>
 :root{
   --bg:oklch(0.968 0.007 55); --surface:oklch(0.996 0.002 55); --raise:oklch(0.985 0.005 55);
   --line:oklch(0.895 0.009 55); --ink:oklch(0.22 0.020 48); --muted:oklch(0.47 0.018 50);
   --accent:oklch(0.52 0.170 40); --accent-solid:oklch(0.50 0.170 38); --accent-soft:oklch(0.94 0.035 45);
   --on-accent:oklch(0.99 0.01 55);
   --ok:oklch(0.42 0.105 155); --ok-bg:oklch(0.93 0.040 155);
   --warn:oklch(0.50 0.130 70); --warn-bg:oklch(0.94 0.045 75);
   --bad:oklch(0.45 0.150 25); --bad-bg:oklch(0.93 0.045 25);
   --radius:.5rem; --ease:cubic-bezier(.22,1,.36,1);
 }
 @media (prefers-color-scheme:dark){
   :root{
     --bg:oklch(0.165 0.012 50); --surface:oklch(0.205 0.014 50); --raise:oklch(0.24 0.014 50);
     --line:oklch(0.30 0.014 50); --ink:oklch(0.93 0.008 60); --muted:oklch(0.70 0.012 60);
     --accent:oklch(0.78 0.140 55); --accent-solid:oklch(0.72 0.150 50); --accent-soft:oklch(0.29 0.050 48);
     --on-accent:oklch(0.19 0.030 50);
     --ok:oklch(0.78 0.110 160); --ok-bg:oklch(0.28 0.050 160);
     --warn:oklch(0.82 0.110 75); --warn-bg:oklch(0.30 0.050 75);
     --bad:oklch(0.82 0.115 25); --bad-bg:oklch(0.30 0.060 25);
   }
 }
 *{box-sizing:border-box}
 html{color-scheme:light dark}
 body{
   margin:0;background:var(--bg);color:var(--ink);
   font:15px/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;
   -webkit-text-size-adjust:100%;
 }
 h1,h2,h3{margin:0;font-weight:620;letter-spacing:-.015em;text-wrap:balance}
 h1{font-size:1.35rem}
 h2{font-size:1.02rem}
 a{color:var(--accent)}
 code{font:0.84em/1.4 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
      background:var(--raise);border:1px solid var(--line);border-radius:.3rem;padding:.1rem .35rem}
 :focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:3px}

 .wrap{max-width:64rem;margin:0 auto;padding:1.5rem 1.25rem 4rem}
 .wrap.narrow{max-width:52rem}

 /* Shell navigation */
 .topbar{display:flex;flex-wrap:wrap;gap:.75rem 1rem;align-items:center;justify-content:space-between;
         padding-bottom:1rem;border-bottom:1px solid var(--line);margin-bottom:1.5rem}
 .brand{display:flex;align-items:baseline;gap:.45rem;text-decoration:none;color:var(--ink);white-space:nowrap}
 .brand h1{display:flex;align-items:center;gap:.35rem;font-size:1.2rem}
 .crow{color:var(--accent);font-weight:700}
 .ver{font-size:.68rem;font-weight:600;color:var(--muted);border:1px solid var(--line);
      border-radius:999px;padding:.05rem .4rem;font-variant-numeric:tabular-nums}
 .nav{display:flex;flex-wrap:wrap;gap:.12rem;align-items:center}
 .nav a{display:inline-flex;align-items:center;gap:.28rem;padding:.34rem .48rem;border-radius:var(--radius);
        text-decoration:none;color:var(--muted);font-size:.825rem;font-weight:530;white-space:nowrap;
        border:1px solid transparent;transition:background 140ms var(--ease),color 140ms var(--ease)}
 .nav a:hover{color:var(--ink);background:var(--surface)}
 .nav a.active{color:var(--ink);background:var(--surface);border-color:var(--line);font-weight:600}
 .nav a.active .pill{background:var(--accent-soft);color:var(--accent)}
 .pill{font-size:.7rem;font-variant-numeric:tabular-nums;padding:.02rem .36rem;border-radius:999px;
       background:var(--raise);color:var(--muted);font-weight:600}

 /* Buttons & forms */
 .btn{
   display:inline-flex;align-items:center;gap:.4rem;border:1px solid var(--line);
   background:var(--surface);color:var(--ink);border-radius:var(--radius);
   padding:.45rem .8rem;font:inherit;font-size:.875rem;font-weight:550;text-decoration:none;cursor:pointer;
   transition:background 140ms var(--ease),border-color 140ms var(--ease),transform 120ms var(--ease);
 }
 .btn:hover{background:var(--raise);border-color:var(--muted)}
 .btn:active{transform:translateY(1px)}
 .btn.primary{background:var(--accent-solid);border-color:transparent;color:var(--on-accent)}
 .btn.primary:hover{filter:brightness(1.07)}
 .btn.ghost{border-color:transparent;background:none;color:var(--muted);padding:.25rem .5rem;font-size:.8125rem;font-weight:500}
 .btn.ghost:hover{background:var(--raise);border-color:var(--line);color:var(--ink)}
 .btn.sm{padding:.3rem .6rem;font-size:.8125rem}

 .field{display:block;margin-bottom:.95rem}
 .field span{display:block;font-size:.8125rem;font-weight:530;color:var(--muted);margin-bottom:.3rem}
 .field input,.field select,.field textarea{
   width:100%;padding:.52rem .7rem;font:inherit;color:var(--ink);
   background:var(--bg);border:1px solid var(--line);border-radius:var(--radius);
 }
 .field textarea{min-height:4.5rem;resize:vertical}
 .field input:focus,.field select:focus,.field textarea:focus{
   outline:2px solid var(--accent);outline-offset:1px;border-color:transparent;background:var(--surface)
 }
 .field .note{display:block;margin-top:.3rem;color:var(--muted);font-size:.75rem;line-height:1.45}
 .field:last-of-type{margin-bottom:0}
 .row2{display:grid;grid-template-columns:1fr;gap:.85rem}
 @media (min-width:36rem){.row2{grid-template-columns:1fr 1fr}}

 /* Structural blocks */
 .panel{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:1.1rem 1.25rem;margin-bottom:1.25rem}
 .panel-head{display:flex;flex-wrap:wrap;gap:.5rem 1rem;align-items:baseline;justify-content:space-between;margin-bottom:.85rem}
 .panel .hint{color:var(--muted);font-size:.8125rem;margin:0}

 .stats{display:grid;grid-template-columns:repeat(2,1fr);gap:1rem 1.25rem;padding:1rem 1.25rem;margin-bottom:1.25rem;
        background:var(--surface);border:1px solid var(--line);border-radius:var(--radius)}
 .stat b{display:block;font-size:1.45rem;font-weight:620;letter-spacing:-.02em;font-variant-numeric:tabular-nums;line-height:1.2}
 .stat span{font-size:.8rem;color:var(--muted)}
 @media (min-width:46rem){
   .stats{grid-template-columns:repeat(5,1fr);gap:0}
   .stats.four{grid-template-columns:repeat(4,1fr)}
   .stat+.stat{border-left:1px solid var(--line);padding-left:1.25rem}
 }

 /* Companion status strip */
 .modules{display:grid;grid-template-columns:1fr;gap:.65rem;margin-bottom:1.5rem}
 @media (min-width:44rem){.modules{grid-template-columns:repeat(4,1fr)}}
 .mod{display:flex;flex-direction:column;gap:.2rem;padding:.75rem .95rem;border:1px solid var(--line);
      border-radius:var(--radius);background:var(--surface);text-decoration:none;color:var(--ink);
      transition:border-color 140ms var(--ease),background 140ms var(--ease)}
 .mod:hover{border-color:var(--muted);background:var(--raise)}
 .mod-top{display:flex;align-items:center;justify-content:space-between;gap:.5rem;font-size:.8125rem;font-weight:600}
 .mod-sub{font-size:.75rem;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
 .dot{display:inline-flex;align-items:center;gap:.35rem;font-size:.72rem;font-weight:580}
 .dot::before{content:"";width:7px;height:7px;border-radius:50%;background:var(--muted)}
 .dot.ok{color:var(--ok)} .dot.ok::before{background:var(--ok)}
 .dot.warn{color:var(--warn)} .dot.warn::before{background:var(--warn)}
 .dot.off{color:var(--muted)}

 /* Tables */
 .card-table{margin-bottom:1.25rem;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);overflow:hidden}
 .card-table>header{display:flex;flex-wrap:wrap;gap:.5rem 1rem;align-items:baseline;justify-content:space-between;
                    padding:.8rem 1.25rem;border-bottom:1px solid var(--line);background:var(--raise)}
 .card-table>header p{margin:0;color:var(--muted);font-size:.8125rem;font-variant-numeric:tabular-nums}
 table{width:100%;border-collapse:collapse;font-size:.9125rem}
 td,th{text-align:left;padding:.55rem 1.25rem;border-bottom:1px solid var(--line)}
 tr:last-child td{border-bottom:0}
 tbody tr{transition:background 120ms var(--ease)}
 tbody tr:hover{background:var(--raise)}
 th{font-size:.75rem;font-weight:580;color:var(--muted);text-transform:none}
 td.num{color:var(--muted);font-variant-numeric:tabular-nums;width:4.5rem}
 td.size,td.when{color:var(--muted);font-size:.8125rem;white-space:nowrap;font-variant-numeric:tabular-nums}
 td.state{width:1%;white-space:nowrap;text-align:right}
 .tag{display:inline-block;padding:.1rem .48rem;border-radius:999px;font-size:.72rem;font-weight:580;
      background:var(--ok-bg);color:var(--ok)}
 .tag.muted{background:var(--raise);color:var(--muted);border:1px solid var(--line)}
 .tag.accent{background:var(--accent-soft);color:var(--accent)}
 form.inline{display:inline;margin-left:.3rem}
 .flash{padding:.7rem 1rem;border-radius:var(--radius);margin-bottom:1.25rem;font-size:.875rem}
 .flash.ok{background:var(--ok-bg);color:var(--ok)}
 .flash.bad{background:var(--bad-bg);color:var(--bad)}
 .empty{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:1.75rem 1.5rem;max-width:42rem}
 .empty h2{margin-bottom:.45rem}
 .empty p{color:var(--muted);margin:.45rem 0 0;font-size:.9rem}
 .empty ol{color:var(--muted);margin:.75rem 0 0;padding-left:1.15rem;font-size:.9rem}
 .empty li{margin:.3rem 0}
 @media (max-width:42rem){
   td.size,th.size{display:none}
   td,th{padding-inline:.85rem}
 }
 @media (prefers-reduced-motion:reduce){*{transition-duration:1ms!important;animation-duration:1ms!important}}
</style>`

const navHeader = `
<header class="topbar">
  <a class="brand" href="/">
    <h1><span class="crow">八</span> Yatagarasu</h1>
    <span class="ver">v{{.Nav.Version}}</span>
  </a>
  <nav class="nav" aria-label="Main">
    <a href="/"{{if eq .Nav.Active "shelf"}} class="active"{{end}}>Shelf{{if .Nav.ShelfCount}} <span class="pill">{{.Nav.ShelfCount}}</span>{{end}}</a>
    <a href="/library"{{if eq .Nav.Active "library"}} class="active"{{end}}>Library{{if .Nav.LibraryCount}} <span class="pill">{{.Nav.LibraryCount}}</span>{{end}}</a>
    <a href="/updates"{{if eq .Nav.Active "updates"}} class="active"{{end}}>Updates</a>
    <a href="/browse"{{if eq .Nav.Active "browse"}} class="active"{{end}}>Browse</a>
    <a href="/downloads"{{if eq .Nav.Active "downloads"}} class="active"{{end}}>Downloads</a>
    <a href="/translate"{{if eq .Nav.Active "translate"}} class="active"{{end}}>Translate</a>
    <a href="/backups"{{if eq .Nav.Active "backups"}} class="active"{{end}}>Backups</a>
    <a href="/settings"{{if eq .Nav.Active "settings"}} class="active"{{end}}>Settings</a>
  </nav>
</header>`

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — companion</title>` + baseHead + `
<style>
 .subbar{display:flex;flex-wrap:wrap;gap:.75rem 1rem;align-items:center;justify-content:space-between;margin-bottom:1.1rem}
 .subbar p{margin:0;color:var(--muted);font-size:.875rem}
 .cols{display:grid;grid-template-columns:1fr;gap:1.25rem;margin-bottom:1.5rem;align-items:start}
 @media (min-width:52rem){.cols{grid-template-columns:1.55fr 1fr}}
 .chart{display:block;width:100%;height:5.75rem;margin-bottom:.45rem}
 .chart .track{fill:var(--line);opacity:.45}
 .chart .bar{fill:var(--accent-solid)}
 .chart .bar.today{fill:var(--accent)}
 .axis{display:flex;justify-content:space-between;margin:0 0 .6rem;color:var(--muted);font-size:.75rem}
 .axis b{color:var(--ink);font-weight:600}
 .recent{list-style:none;margin:0;padding:0}
 .recent li{display:flex;justify-content:space-between;gap:1rem;padding:.45rem 0;border-bottom:1px solid var(--line)}
 .recent li:last-child{border-bottom:0}
 .recent b{font-weight:550;font-size:.9rem}
 .recent em{display:block;font-style:normal;color:var(--muted);font-size:.8rem}
 .recent time{color:var(--muted);font-size:.8rem;white-space:nowrap}
</style>
<body><div class="wrap">
` + navHeader + `

<div class="subbar">
  <p>KOReader shelf, library sync hub, Suwayomi CBZ proxy &amp; translation worker for Karasu.</p>
  <a class="btn primary" href="/plugin.zip" download>Download KOReader plugin</a>
</div>

<div class="modules">
  <a class="mod" href="/settings#suwayomi">
    <div class="mod-top"><span>Suwayomi Bridge</span>
      {{if .Nav.Suwayomi.Configured}}{{if .Nav.Suwayomi.Reachable}}<span class="dot ok">online</span>{{else}}<span class="dot warn">offline</span>{{end}}{{else}}<span class="dot off">off</span>{{end}}
    </div>
    <span class="mod-sub">{{if .Nav.Suwayomi.Reachable}}{{.Nav.Suwayomi.Version}} · CBZ streaming{{else if .Nav.Suwayomi.Configured}}Configured, unreachable{{else}}Optional --profile suwayomi{{end}}</span>
  </a>
  <a class="mod" href="/translate">
    <div class="mod-top"><span>Translation</span>
      {{if .Nav.Translate.Configured}}<span class="dot ok">{{.Nav.Translate.Provider}}</span>{{else}}<span class="dot off">cache only</span>{{end}}
    </div>
    <span class="mod-sub">{{if .Nav.Translate.Configured}}{{.Nav.Translate.Model}} · {{.Nav.CacheCount}} cached{{else}}{{.Nav.CacheCount}} cached lines{{end}}</span>
  </a>
  <a class="mod" href="/library">
    <div class="mod-top"><span>Library Sync</span>
      {{if .Nav.LibraryCount}}<span class="dot ok">active</span>{{else}}<span class="dot off">idle</span>{{end}}
    </div>
    <span class="mod-sub">{{.Nav.LibraryCount}} synced manga</span>
  </a>
  <a class="mod" href="/backups">
    <div class="mod-top"><span>Backups</span>
      {{if .Nav.BackupsCount}}<span class="dot ok">{{.Nav.BackupsCount}} saved</span>{{else}}<span class="dot off">none</span>{{end}}
    </div>
    <span class="mod-sub">.tachibk cloud vault</span>
  </a>
</div>

<div class="stats">
  <div class="stat"><b>{{.Chapters}}</b><span>{{if eq .Chapters 1}}chapter{{else}}chapters{{end}} on the shelf</span></div>
  <div class="stat"><b>{{.MangaN}}</b><span>shelf manga</span></div>
  <div class="stat"><b>{{.Unread}}</b><span>waiting to be read</span></div>
  <div class="stat"><b>{{.Finished7}}</b><span>finished this week</span></div>
  <div class="stat"><b>{{.Size}}</b><span>on disk</span></div>
</div>

<div class="cols">
  <section class="panel" style="margin-bottom:0">
    <div class="panel-head"><h2>Finished chapters</h2><span class="hint">last {{.ChartDays}} days</span></div>
    {{if .HasStats}}
    <svg class="chart" viewBox="0 0 {{.ChartW}} {{.ChartH}}" preserveAspectRatio="none"
         role="img" aria-label="Chapters finished per day over the last {{.ChartDays}} days">
      {{range .Bars}}<g><title>{{.Title}}</title>
        <rect class="track" x="{{.X}}" y="0" width="{{$.BarW}}" height="{{$.ChartH}}"></rect>
        {{if .Count}}<rect class="bar{{if .Today}} today{{end}}" x="{{.X}}" y="{{.Y}}" width="{{$.BarW}}" height="{{$.H}}"></rect>{{end}}
      </g>{{end}}
    </svg>
    <p class="axis"><span>{{.ChartFrom}}</span><b>today</b></p>
    <p class="hint">As reported by KOReader. {{.Finished7}} finished this week.</p>
    {{else}}
    <p class="hint">No chapters finished yet. Once KOReader reports one back, the last {{.ChartDays}} days show up here.</p>
    {{end}}
  </section>

  <section class="panel" style="margin-bottom:0">
    <div class="panel-head"><h2>Recently finished</h2></div>
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
<section class="card-table">
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
          <a class="btn sm" href="/read/shelf/{{.ID}}">read</a>
          {{if .Read}}<span class="tag">read</span>
          <form class="inline" method="post" action="/api/shelf/{{.ID}}/read">
            <input type="hidden" name="read" value="false">
            <button class="btn ghost" type="submit" title="Undo a read report sent by mistake">undo</button>
          </form>{{end}}
          <form class="inline" method="post" action="/api/shelf/{{.ID}}/delete"
                data-confirm="Remove {{.Name}} from the shelf? Karasu re-uploads it on its next sync if it still wants it here.">
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
  <h2>The KOReader shelf is empty</h2>
  <p>Karasu uploads only chapters downloaded <strong>as CBZ</strong>. If a sync ran and nothing arrived:</p>
  <ol>
    <li>Enable <em>Save chapters as CBZ</em> in Karasu's download settings.</li>
    <li>Mark manga with <em>Send to Yatagarasu</em> on their details page.</li>
    <li>Download the chapters — Karasu queues them and pushes them on the next sync.</li>
  </ol>
</section>
{{end}}

</div>
<script nonce="{{.Nonce}}">
for (const f of document.querySelectorAll("form[data-confirm]"))
  f.addEventListener("submit", e => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });
</script>
`))

var libraryTmpl = template.Must(template.New("library").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — library</title>` + baseHead + `
<style>
 .lib-head{display:flex;flex-wrap:wrap;gap:.65rem;align-items:center;justify-content:space-between;margin-bottom:1rem}
 .lib-controls{display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;flex:1;justify-content:flex-end}
 .lib-search{padding:.4rem .7rem;font:inherit;font-size:.85rem;color:var(--ink);background:var(--surface);
             border:1px solid var(--line);border-radius:var(--radius);min-width:12rem;max-width:18rem;flex:1}
 .lib-search:focus{outline:2px solid var(--accent);outline-offset:1px;border-color:transparent}
 .view-seg{display:inline-flex;border:1px solid var(--line);border-radius:var(--radius);overflow:hidden;background:var(--surface)}
 .view-seg button{padding:.36rem .65rem;font:inherit;font-size:.8125rem;font-weight:540;border:0;border-right:1px solid var(--line);
                  background:transparent;color:var(--muted);cursor:pointer}
 .view-seg button:last-child{border-right:0}
 .view-seg button:hover{color:var(--ink);background:var(--raise)}
 .view-seg button.active{background:var(--accent-soft);color:var(--accent);font-weight:600}
 .filters{display:flex;flex-wrap:wrap;gap:.4rem;margin-bottom:1.15rem}
 .filters a{padding:.3rem .68rem;border-radius:999px;border:1px solid var(--line);
            background:var(--surface);color:var(--muted);font-size:.8125rem;text-decoration:none}
 .filters a:hover{border-color:var(--muted);color:var(--ink)}
 .filters a.active{background:var(--accent-soft);border-color:var(--accent);color:var(--accent);font-weight:600}
 .lib-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(9.4rem,1fr));gap:1.05rem .9rem;margin-bottom:1.35rem}
 .lib-card{display:flex;flex-direction:column;text-decoration:none;color:var(--ink)}
 .lib-card.hidden,.lib-row.hidden{display:none}
 .lib-cover{position:relative;aspect-ratio:2/3;border-radius:var(--radius);overflow:hidden;
            background:var(--raise);border:1px solid var(--line);margin-bottom:.42rem;
            transition:transform 150ms var(--ease),border-color 150ms var(--ease)}
 a.lib-card:hover .lib-cover{transform:translateY(-2px);border-color:var(--accent)}
 .lib-cover img{width:100%;height:100%;object-fit:cover;display:block}
 .lib-placeholder{width:100%;height:100%;display:flex;align-items:center;justify-content:center;
                  font-size:2.2rem;font-weight:650;color:var(--muted);background:var(--raise)}
 .lib-badges{position:absolute;top:.4rem;left:.4rem;display:flex;gap:.25rem;flex-wrap:wrap}
 .badge{padding:.1rem .44rem;border-radius:.32rem;font-size:.7rem;font-weight:650;font-variant-numeric:tabular-nums;
        line-height:1.35;box-shadow:0 1px 4px oklch(0 0 0 / .32)}
 .badge.unread{background:var(--accent-solid);color:var(--on-accent)}
 .badge.dl{background:var(--ok);color:oklch(0.16 0.02 155)}
 .lib-title{font-size:.84rem;font-weight:560;line-height:1.3;display:-webkit-box;-webkit-line-clamp:2;
            -webkit-box-orient:vertical;overflow:hidden}
 .lib-sub{font-size:.72rem;color:var(--muted);margin-top:.12rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
 .lib-link{color:var(--ink);text-decoration:none;font-weight:560}
 .lib-link:hover{color:var(--accent)}
 .lib-mini{width:28px;height:40px;border-radius:.25rem;object-fit:cover;vertical-align:middle;margin-right:.55rem;background:var(--raise)}
 .lib-title-cell{display:inline-flex;align-items:center}
 .lib-view-list{display:none}
 .lib-wrap.list-mode .lib-grid{display:none}
 .lib-wrap.list-mode .lib-view-list{display:block}
 .prog{display:flex;align-items:center;gap:.55rem;min-width:8.5rem}
 .bar-bg{flex:1;height:5px;background:var(--line);border-radius:999px;overflow:hidden}
 .bar-fg{height:100%;background:var(--accent-solid);border-radius:999px}
 .prog span{font-size:.78rem;color:var(--muted);font-variant-numeric:tabular-nums;white-space:nowrap}
 .meta-grid{display:grid;grid-template-columns:1fr;gap:1.25rem}
 @media (min-width:48rem){.meta-grid{grid-template-columns:1fr 1fr}}
 .plain-list{list-style:none;margin:0;padding:0;font-size:.85rem}
 .plain-list li{display:flex;justify-content:space-between;gap:.75rem;padding:.45rem 0;border-bottom:1px solid var(--line);word-break:break-all}
 .plain-list li:last-child{border-bottom:0}
</style>
<body><div class="wrap">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<div class="stats">
  <div class="stat"><b>{{.TotalMangas}}</b><span>library manga</span></div>
  <div class="stat"><b>{{len .Categories}}</b><span>categories</span></div>
  <div class="stat"><b>{{.ReadChapters}} / {{.TotalChapters}}</b><span>chapters read</span></div>
  <div class="stat"><b>{{.MergedTotal}}</b><span>merged sources</span></div>
  <div class="stat"><b>{{.LastSync}}</b><span>last reconcile</span></div>
</div>

<div class="lib-head">
  <div>
    <h2>Library{{if .SelectedCat}} · {{.SelectedCat}}{{end}} <span class="pill" id="libCount">{{len .Mangas}}</span></h2>
  </div>
  <div class="lib-controls">
    <input class="lib-search" id="libFilter" type="search" placeholder="Filter library..." aria-label="Filter library">
    <div class="view-seg" role="group" aria-label="View mode">
      <button type="button" id="btnGrid" class="active">Grid</button>
      <button type="button" id="btnList">List</button>
    </div>
    {{if .SuwayomiConnected}}
    <a class="btn sm" href="/categories">Manage Categories</a>
    <form method="post" action="/downloads/control" style="margin:0">
      <input type="hidden" name="action" value="update-library">
      <input type="hidden" name="redirect" value="/library">
      <button class="btn sm primary" type="submit">Update Library</button>
    </form>
    {{end}}
  </div>
</div>

{{if .Categories}}
<div class="filters">
  <a href="/library"{{if eq .SelectedCat ""}} class="active"{{end}}>All ({{.TotalMangas}})</a>
  {{range .Categories}}
  <a href="/library?cat={{.Name}}"{{if eq $.SelectedCat .Name}} class="active"{{end}}>{{.Name}}</a>
  {{end}}
</div>
{{end}}

{{if .Mangas}}
<div class="lib-wrap" id="libWrap">
  <div class="lib-grid" id="libGrid">
    {{range .Mangas}}
    {{if gt .SuwayomiID 0}}
    <a class="lib-card" href="/manga/{{.SuwayomiID}}" data-title="{{.Title}}" data-cats="{{range .Categories}}{{.}} {{end}}">
    {{else}}
    <div class="lib-card" data-title="{{.Title}}" data-cats="{{range .Categories}}{{.}} {{end}}">
    {{end}}
      <div class="lib-cover">
        {{if .ThumbnailURL}}<img src="{{.ThumbnailURL}}" alt="{{.Title}}" loading="lazy">{{else}}<div class="lib-placeholder">{{.Initial}}</div>{{end}}
        {{if or (gt .UnreadCount 0) (gt .DownloadCount 0)}}
        <div class="lib-badges">
          {{if gt .UnreadCount 0}}<span class="badge unread" title="{{.UnreadCount}} unread">{{.UnreadCount}}</span>{{end}}
          {{if gt .DownloadCount 0}}<span class="badge dl" title="{{.DownloadCount}} downloaded">{{.DownloadCount}}</span>{{end}}
        </div>
        {{end}}
      </div>
      <span class="lib-title">{{.Title}}</span>
      {{if .Categories}}<span class="lib-sub">{{range $i, $c := .Categories}}{{if $i}} · {{end}}{{$c}}{{end}}</span>{{end}}
    {{if gt .SuwayomiID 0}}</a>{{else}}</div>{{end}}
    {{end}}
  </div>

  <section class="card-table lib-view-list">
    <header>
      <h2>Library{{if .SelectedCat}} · {{.SelectedCat}}{{end}}</h2>
      <p>{{len .Mangas}} {{if eq (len .Mangas) 1}}title{{else}}titles{{end}}</p>
    </header>
    <table>
      <thead><tr><th>Title</th><th>Categories</th><th>Progress</th><th class="size">Unread / DL</th><th>Updated</th></tr></thead>
      <tbody>
        {{range .Mangas}}
        <tr class="lib-row" data-title="{{.Title}}" data-cats="{{range .Categories}}{{.}} {{end}}">
          <td>
            <span class="lib-title-cell">
              {{if .ThumbnailURL}}<img class="lib-mini" src="{{.ThumbnailURL}}" alt="" loading="lazy">{{end}}
              {{if gt .SuwayomiID 0}}<a class="lib-link" href="/manga/{{.SuwayomiID}}">{{.Title}}</a>{{else}}<strong>{{.Title}}</strong>{{end}}
            </span>
          </td>
          <td>{{range .Categories}}<span class="tag muted">{{.}}</span> {{end}}</td>
          <td>
            <div class="prog">
              <div class="bar-bg"><div class="bar-fg" style="width:{{.ProgressPct}}%"></div></div>
              <span>{{.ReadCount}}/{{.TotalCount}}</span>
            </div>
          </td>
          <td class="size">
            {{if gt .UnreadCount 0}}<span class="tag accent">{{.UnreadCount}} unread</span> {{end}}
            {{if gt .DownloadCount 0}}<span class="tag">{{.DownloadCount}} dl</span>{{end}}
            {{if and (eq .UnreadCount 0) (eq .DownloadCount 0)}}—{{end}}
          </td>
          <td class="when">{{.Updated}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </section>
</div>
{{else}}
<section class="empty">
  <h2>No library titles yet</h2>
  <p>{{if .SuwayomiConnected}}Add manga from <a href="/browse">Browse</a> or sync from Karasu under <em>Settings → Yatagarasu</em>.{{else}}Connect Suwayomi in <a href="/settings#suwayomi">Settings</a> or enable <strong>Sync library, progress &amp; extensions</strong> in Karasu.{{end}}</p>
</section>
{{end}}

<div class="meta-grid" style="margin-top:1.25rem">
  <section class="panel" style="margin-bottom:0">
    <div class="panel-head"><h2>Extension repositories</h2><span class="pill">{{len .Repos}}</span></div>
    {{if .Repos}}
    <ul class="plain-list">{{range .Repos}}<li><code>{{.}}</code></li>{{end}}</ul>
    {{else}}<p class="hint">No extension repositories synced yet.</p>{{end}}
  </section>

  <section class="panel" style="margin-bottom:0">
    <div class="panel-head"><h2>Known extensions</h2><span class="pill">{{len .Extensions}}</span></div>
    {{if .Extensions}}
    <ul class="plain-list">
      {{range .Extensions}}<li><span><strong>{{.Name}}</strong> <span class="tag muted">{{.Lang}}</span></span><span class="hint">v{{.VersionCode}}</span></li>{{end}}
    </ul>
    {{else}}<p class="hint">No extensions reported by connected devices yet.</p>{{end}}
  </section>
</div>

</div>
<script nonce="{{.Nonce}}">
(function(){
  const wrap = document.getElementById("libWrap");
  const filter = document.getElementById("libFilter");
  const btnGrid = document.getElementById("btnGrid");
  const btnList = document.getElementById("btnList");
  const countEl = document.getElementById("libCount");
  const cards = Array.from(document.querySelectorAll(".lib-card"));
  const rows = Array.from(document.querySelectorAll(".lib-row"));

  function setView(v) {
    if (!wrap) return;
    const isList = v === "list";
    wrap.classList.toggle("list-mode", isList);
    if (btnGrid) btnGrid.classList.toggle("active", !isList);
    if (btnList) btnList.classList.toggle("active", isList);
    try { localStorage.setItem("yata_library_view", isList ? "list" : "grid"); } catch (e) {}
  }

  if (btnGrid) btnGrid.addEventListener("click", () => setView("grid"));
  if (btnList) btnList.addEventListener("click", () => setView("list"));
  try {
    if (localStorage.getItem("yata_library_view") === "list") setView("list");
  } catch (e) {}

  if (filter) {
    filter.addEventListener("input", () => {
      const q = filter.value.trim().toLowerCase();
      let shown = 0;
      cards.forEach(el => {
        const hay = ((el.dataset.title || "") + " " + (el.dataset.cats || "")).toLowerCase();
        const match = !q || hay.includes(q);
        el.classList.toggle("hidden", !match);
        if (match) shown++;
      });
      rows.forEach(el => {
        const hay = ((el.dataset.title || "") + " " + (el.dataset.cats || "")).toLowerCase();
        el.classList.toggle("hidden", !(!q || hay.includes(q)));
      });
      if (countEl) countEl.textContent = String(shown);
    });
  }
})();
</script>
`))

var translateTmpl = template.Must(template.New("translate").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — translation worker</title>` + baseHead + `
<style>
 .result-box{padding:.9rem 1rem;border-radius:var(--radius);background:var(--raise);border:1px solid var(--line);margin-top:.85rem}
 .result-box span{display:block;font-size:.75rem;color:var(--muted);margin-bottom:.25rem}
 .result-box p{margin:0;font-size:1rem;font-weight:530}
 .actions-row{display:flex;flex-wrap:wrap;gap:.65rem;align-items:center;justify-content:space-between}
</style>
<body><div class="wrap narrow">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<div class="stats four">
  <div class="stat"><b>{{if .Provider}}{{.Provider}}{{else}}off{{end}}</b><span>active provider</span></div>
  <div class="stat"><b>{{.CachedLines}}</b><span>cached lines</span></div>
  <div class="stat"><b>{{.ChapterFiles}}</b><span>translated chapters</span></div>
  <div class="stat"><b>{{.ContextFiles}}</b><span>series glossaries</span></div>
</div>

<section class="panel">
  <div class="panel-head">
    <h2>Server-side translation worker</h2>
    <a class="btn sm" href="/settings#translate">Configure provider</a>
  </div>
  {{if .Provider}}
  <p class="hint">Using <code>{{.Provider}}</code>{{if .Model}} with model <code>{{.Model}}</code>{{end}}{{if .Endpoint}} at <code>{{.Endpoint}}</code>{{end}}. Select <strong>Yatagarasu (companion server)</strong> in Karasu under <em>Settings → Translation</em>.</p>
  {{else}}
  <p class="hint">No server LLM provider configured yet. The shared cache still works across devices; configure OpenRouter, DeepSeek, Gemini or local Ollama in <a href="/settings#translate">Settings</a> to offload translation.</p>
  {{end}}
</section>

<section class="panel">
  <div class="panel-head"><h2>Test translation</h2><span class="hint">Calls the configured LLM directly</span></div>
  <form method="post" action="/translate">
    <div class="row2">
      <label class="field"><span>From language</span>
        <input name="fromLang" value="{{.FromLang}}" placeholder="en">
      </label>
      <label class="field"><span>To language (BCP-47)</span>
        <input name="toLang" value="{{.ToLang}}" placeholder="pt-BR">
      </label>
    </div>
    <label class="field"><span>Sample bubble text</span>
      <textarea name="sample" placeholder="I won't forgive you for hurting my friends!">{{.Sample}}</textarea>
    </label>
    <div class="actions-row">
      <button class="btn primary" type="submit">Translate sample</button>
    </div>
  </form>
  {{if .Result}}
  <div class="result-box">
    <span>Translation result</span>
    <p>{{.Result}}</p>
  </div>
  {{end}}
</section>

<section class="panel">
  <div class="actions-row">
    <div>
      <h2>Shared line cache</h2>
      <p class="hint" style="margin-top:.2rem">{{.CachedLines}} recurring lines cached across all connected Karasu devices.</p>
    </div>
    <form method="post" action="/translate" data-confirm="Clear {{.CachedLines}} cached translation lines?">
      <input type="hidden" name="action" value="clear-cache">
      <button class="btn" type="submit">Clear cache</button>
    </form>
  </div>
</section>

</div>
<script nonce="{{.Nonce}}">
for (const f of document.querySelectorAll("form[data-confirm]"))
  f.addEventListener("submit", e => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });
</script>
`))

var backupsTmpl = template.Must(template.New("backups").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — backups</title>` + baseHead + `
<style>
 .upload-form{display:flex;flex-wrap:wrap;gap:.75rem;align-items:center}
 .upload-form input[type=file]{font-size:.875rem;color:var(--muted)}
</style>
<body><div class="wrap">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<div class="stats four">
  <div class="stat"><b>{{len .Backups}}</b><span>stored backups</span></div>
  <div class="stat"><b>{{.TotalSize}}</b><span>total size</span></div>
  <div class="stat"><b>{{.MaxBackups}}</b><span>retention limit</span></div>
  <div class="stat"><b>.tachibk</b><span>Mihon / Karasu format</span></div>
</div>

<section class="panel">
  <div class="panel-head"><h2>Upload backup</h2><span class="hint">Oldest archives above the retention limit are pruned automatically</span></div>
  <form class="upload-form" method="post" action="/backups/upload" enctype="multipart/form-data">
    <input type="file" name="file" accept=".tachibk,.gz,.proto.gz" required>
    <button class="btn primary" type="submit">Upload .tachibk</button>
  </form>
</section>

{{if .Backups}}
<section class="card-table">
  <header>
    <h2>Saved backups</h2>
    <p>{{len .Backups}} {{if eq (len .Backups) 1}}archive{{else}}archives{{end}} · {{.TotalSize}}</p>
  </header>
  <table>
    <thead><tr><th>Filename</th><th class="size">Size</th><th>Created</th><th></th></tr></thead>
    <tbody>
      {{range .Backups}}
      <tr>
        <td><code>{{.Name}}</code></td>
        <td class="size">{{.Size}}</td>
        <td class="when">{{.Created}}</td>
        <td class="state">
          <a class="btn sm" href="/api/backups/{{.ID}}" download>download</a>
          <form class="inline" method="post" action="/backups/{{.ID}}/delete"
                data-confirm="Delete backup {{.Name}} permanently?">
            <button class="btn ghost" type="submit">delete</button>
          </form>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
</section>
{{else}}
<section class="empty">
  <h2>No remote backups yet</h2>
  <p>Upload a <code>.tachibk</code> archive above or push one directly from Karasu.</p>
</section>
{{end}}

</div>
<script nonce="{{.Nonce}}">
for (const f of document.querySelectorAll("form[data-confirm]"))
  f.addEventListener("submit", e => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });
</script>
`))

var settingsTmpl = template.Must(template.New("settings").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — settings</title>` + baseHead + `
<style>
 .choices{display:flex;gap:.5rem;flex-wrap:wrap;margin-bottom:.3rem}
 .choices label{display:inline-flex;align-items:center;gap:.4rem;padding:.4rem .75rem;cursor:pointer;white-space:nowrap;
                border:1px solid var(--line);border-radius:var(--radius);background:var(--bg);font-size:.875rem}
 .choices label:has(input:checked){border-color:var(--accent);background:var(--raise);font-weight:550}
 .choices input{width:auto;margin:0;accent-color:var(--accent-solid)}
 .save{display:flex;gap:.75rem;align-items:center;flex-wrap:wrap;margin-bottom:1.5rem}
 .facts{list-style:none;margin:0;padding:0;font-size:.875rem}
 .facts li{display:flex;justify-content:space-between;gap:1rem;padding:.42rem 0;border-bottom:1px solid var(--line)}
 .facts li:last-child{border-bottom:0}
 .facts span{color:var(--muted)}
 .facts b{font-weight:550;font-variant-numeric:tabular-nums;text-align:right}
 .danger{border-color:oklch(0.62 0.150 25 / .45)}
 .danger .row{display:flex;flex-wrap:wrap;gap:.75rem 1rem;align-items:center;justify-content:space-between;
              padding:.7rem 0;border-bottom:1px solid var(--line)}
 .danger .row:last-child{border-bottom:0;padding-bottom:0}
 .danger .row p{margin:0;font-size:.8125rem;color:var(--muted);max-width:26rem}
 .danger .row strong{display:block;color:var(--ink);font-weight:550;font-size:.9375rem}
</style>
<body><div class="wrap narrow">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<form method="post" action="/settings">

  <section class="panel">
    <div class="panel-head"><h2>Access &amp; plugin</h2><span class="hint">Stored in {{.DataDir}}/settings.json</span></div>
    <label class="field"><span>Public URL</span>
      <input name="publicUrl" value="{{.PublicURL}}" placeholder="http://192.168.0.10:3080"
             inputmode="url" autocapitalize="off" autocorrect="off" spellcheck="false">
      <em class="note">Baked into the KOReader plugin ZIP when downloaded. Leave blank to use the browser's host.</em>
    </label>
    <label class="field"><span>API key</span>
      <input name="apiKey" value="{{.APIKey}}" autocomplete="off" autocapitalize="off"
             autocorrect="off" spellcheck="false">
      <em class="note">Blank accepts unauthenticated requests — fine on a trusted LAN, nowhere else.</em>
    </label>
  </section>

  <section class="panel" id="suwayomi">
    <div class="panel-head">
      <h2>Suwayomi-Server bridge</h2>
      {{if .Nav.Suwayomi.Configured}}{{if .Nav.Suwayomi.Reachable}}<span class="dot ok">connected ({{.Nav.Suwayomi.Version}})</span>{{else}}<span class="dot warn">unreachable</span>{{end}}{{else}}<span class="dot off">disabled</span>{{end}}
    </div>
    <p class="hint" style="margin-bottom:.9rem">Connects to an official Suwayomi-Server (no fork needed) for 24/7 extension execution and LAN CBZ streaming. Start the bundled container with <code>docker compose --profile suwayomi up -d</code>.</p>
    <label class="field"><span>Suwayomi URL</span>
      <input name="suwayomiUrl" value="{{.SuwayomiURL}}" placeholder="http://suwayomi:4567"
             inputmode="url" autocapitalize="off" autocorrect="off" spellcheck="false">
    </label>
    <div class="row2">
      <label class="field"><span>Username (optional)</span>
        <input name="suwayomiUser" value="{{.SuwayomiUser}}" autocomplete="off">
      </label>
      <label class="field"><span>Password (optional)</span>
        <input name="suwayomiPass" type="password" value="{{.SuwayomiPass}}" autocomplete="off">
      </label>
    </div>
  </section>

  <section class="panel" id="translate">
    <div class="panel-head">
      <h2>Manga translation worker</h2>
      {{if .TranslateProvider}}<span class="dot ok">{{.TranslateProvider}}</span>{{else}}<span class="dot off">disabled</span>{{end}}
    </div>
    <p class="hint" style="margin-bottom:.9rem">Offloads bubble translation from phones and shares the line cache across devices. Supports OpenRouter, DeepSeek, Google Gemini, or local Ollama/vLLM.</p>
    <div class="row2">
      <label class="field"><span>Provider</span>
        <select name="translateProvider">
          <option value=""{{if eq .TranslateProvider ""}} selected{{end}}>Disabled (cache only)</option>
          <option value="openai"{{if eq .TranslateProvider "openai"}} selected{{end}}>OpenAI-compatible (OpenRouter / DeepSeek / Ollama)</option>
          <option value="gemini"{{if eq .TranslateProvider "gemini"}} selected{{end}}>Google Gemini API</option>
        </select>
      </label>
      <label class="field"><span>Model</span>
        <input name="translateModel" value="{{.TranslateModel}}" placeholder="google/gemma-4-31b-it:free"
               autocapitalize="off" autocorrect="off" spellcheck="false">
      </label>
    </div>
    <label class="field"><span>Endpoint URL (optional)</span>
      <input name="translateEndpoint" value="{{.TranslateEndpoint}}" placeholder="https://openrouter.ai/api/v1/chat/completions or http://ollama:11434/v1/chat/completions"
             inputmode="url" autocapitalize="off" autocorrect="off" spellcheck="false">
    </label>
    <label class="field"><span>Provider API key</span>
      <input name="translateApiKey" type="password" value="{{.TranslateAPIKey}}" autocomplete="off"
             placeholder="Leave blank for local Ollama/vLLM">
    </label>
    <div class="row2" style="margin-top:1rem;padding-top:1rem;border-top:1px solid var(--line)">
      <label class="field"><span>Image Translator sidecar URL (manga-image-translator)</span>
        <input name="imageTranslatorUrl" value="{{.ImageTranslatorURL}}" placeholder="http://manga-translator:8000"
               inputmode="url" autocapitalize="off" autocorrect="off" spellcheck="false">
        <em class="note">Full-page OCR + LaMa inpainting + typesetting via <code>docker compose --profile translator up -d</code>.</em>
      </label>
      <label class="field"><span>Default target language</span>
        <input name="imageTranslatorLang" value="{{.ImageTranslatorLang}}" placeholder="PTB"
               autocapitalize="characters" autocorrect="off" spellcheck="false">
        <em class="note">3-letter code used by manga-image-translator (e.g. <code>PTB</code>, <code>ENG</code>, <code>ESP</code>).</em>
      </label>
    </div>
  </section>

  <section class="panel">
    <div class="panel-head"><h2>Time, history &amp; backups</h2><span class="hint">{{.Now}}</span></div>
    <div class="row2">
      <label class="field"><span>Timezone</span>
        <input name="timezone" value="{{.Timezone}}" placeholder="America/Sao_Paulo" list="tz"
               autocapitalize="off" autocorrect="off" spellcheck="false">
        <datalist id="tz">
          {{range .Zones}}<option value="{{.}}">{{end}}
        </datalist>
      </label>
      <label class="field"><span>Max .tachibk backups kept</span>
        <input name="maxBackups" type="number" min="1" max="100" value="{{.MaxBackups}}">
      </label>
    </div>
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
    <button class="btn primary" type="submit">Save settings</button>
    <a class="btn ghost" href="/">Cancel</a>
  </div>
</form>

<section class="panel">
  <div class="panel-head"><h2>On disk</h2><span class="hint">Yatagarasu v{{.Version}}</span></div>
  <ul class="facts">
    <li><span>Chapters on KOReader shelf</span><b>{{.Entries}}</b></li>
    <li><span>Recorded read events</span><b>{{.Events}}</b></li>
    <li><span>Synced library manga</span><b>{{.Nav.LibraryCount}}</b></li>
    <li><span>Cached translation lines / chapters</span><b>{{.CachedLines}} / {{.ChapterFiles}}</b></li>
    <li><span>Stored .tachibk backups</span><b>{{.Nav.BackupsCount}}</b></li>
    <li><span>Data directory</span><b>{{.DataDir}}</b></li>
  </ul>
</section>

<section class="panel danger">
  <div class="panel-head"><h2>Danger zone</h2><span class="hint">Never deletes files already on your e-reader</span></div>
  <div class="row">
    <div>
      <strong>Clear reading history</strong>
      <p>Empties the activity chart and recently-finished list. Read flags on chapters stay.</p>
    </div>
    <form method="post" action="/settings" data-confirm="Clear {{.Events}} recorded read events?">
      <input type="hidden" name="action" value="clear-history">
      <button class="btn" type="submit">Clear history</button>
    </form>
  </div>
  <div class="row">
    <div>
      <strong>Clear translation cache</strong>
      <p>Drops {{.CachedLines}} cached lines. Already-translated chapters on devices are unaffected.</p>
    </div>
    <form method="post" action="/settings" data-confirm="Clear {{.CachedLines}} cached translation lines?">
      <input type="hidden" name="action" value="clear-translation-cache">
      <button class="btn" type="submit">Clear cache</button>
    </form>
  </div>
  {{if .HasKey}}
  <div class="row">
    <div>
      <strong>Log out of this browser</strong>
      <p>Drops the session cookie. The API key itself is unchanged.</p>
    </div>
    <form method="post" action="/logout">
      <button class="btn" type="submit">Log out</button>
    </form>
  </div>
  {{end}}
  <div class="row">
    <div>
      <strong>Empty the shelf</strong>
      <p>Deletes all {{.Entries}} CBZ files and metadata on the shelf. Karasu re-uploads on the next sync.</p>
    </div>
    <form method="post" action="/settings" data-confirm="Delete all {{.Entries}} chapters from the shelf?">
      <input type="hidden" name="action" value="empty-shelf">
      <button class="btn" type="submit">Empty shelf</button>
    </form>
  </div>
</section>

</div>
<script nonce="{{.Nonce}}">
for (const f of document.querySelectorAll("form[data-confirm]"))
  f.addEventListener("submit", e => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });
</script>
`))

var gateTmpl = template.Must(template.New("gate").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu</title>` + baseHead + `
<style>
  .gate{max-width:22rem;margin:18vh auto;padding:0 1.5rem}
  .gate h1{margin-bottom:.25rem}
  .gate p{color:var(--muted);margin-bottom:1.5rem}
  .gate .bad{color:var(--bad);font-weight:550}
</style>
<body><main class="gate">
  <h1><span class="crow">八</span> Yatagarasu</h1>
  {{if .Problem}}<p class="bad">{{.Problem}}</p>
  {{else}}<p>This companion is protected. Enter its API key to continue.</p>{{end}}
  <form method="post" action="/login">
    <label class="field"><span>API key</span>
      <input name="key" type="password" autofocus autocomplete="current-password">
    </label>
    <button class="btn primary" type="submit">Open Yatagarasu</button>
  </form>
</main>
`))

var browseTmpl = template.Must(template.New("browse").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — browse</title>` + baseHead + `
<style>
 .tabs{display:flex;gap:.35rem;border-bottom:1px solid var(--line);margin-bottom:1.25rem}
 .tabs a{padding:.55rem .95rem;text-decoration:none;color:var(--muted);font-size:.9rem;font-weight:550;
         border-bottom:2px solid transparent;margin-bottom:-1px;transition:color 140ms var(--ease),border-color 140ms var(--ease)}
 .tabs a:hover{color:var(--ink)}
 .tabs a.active{color:var(--ink);border-bottom-color:var(--accent)}
 .toolbar{display:flex;flex-wrap:wrap;gap:.6rem;align-items:center;justify-content:space-between;margin-bottom:1.15rem}
 .toolbar-left{display:flex;flex-wrap:wrap;gap:.45rem;align-items:center}
 .seg{display:inline-flex;border:1px solid var(--line);border-radius:var(--radius);overflow:hidden;background:var(--surface)}
 .seg a{padding:.38rem .7rem;font-size:.825rem;font-weight:530;text-decoration:none;color:var(--muted);border-right:1px solid var(--line)}
 .seg a:last-child{border-right:0}
 .seg a:hover{color:var(--ink);background:var(--raise)}
 .seg a.active{background:var(--accent-soft);color:var(--accent);font-weight:600}
 .search-bar{display:flex;gap:.4rem;align-items:center;flex:1;min-width:13rem;max-width:26rem}
 .search-bar input,.source-select,.lang-select,.repo-input{padding:.4rem .65rem;font:inherit;font-size:.85rem;color:var(--ink);
   background:var(--surface);border:1px solid var(--line);border-radius:var(--radius)}
 .search-bar input,.repo-input{flex:1;min-width:0}
 .source-select{max-width:16rem}
 .manga-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(9.25rem,1fr));gap:1rem .85rem;margin-bottom:1.25rem}
 .manga-card{display:flex;flex-direction:column;text-decoration:none;color:var(--ink)}
 .cover-wrap{position:relative;aspect-ratio:2/3;border-radius:var(--radius);overflow:hidden;
             background:var(--raise);border:1px solid var(--line);margin-bottom:.42rem;
             transition:transform 150ms var(--ease),border-color 150ms var(--ease)}
 .manga-card:hover .cover-wrap{transform:translateY(-2px);border-color:var(--accent)}
 .cover-wrap img{width:100%;height:100%;object-fit:cover;display:block}
 .cover-badge{position:absolute;top:.4rem;left:.4rem;padding:.12rem .45rem;border-radius:999px;
              font-size:.68rem;font-weight:620;background:var(--accent-solid);color:var(--on-accent);
              box-shadow:0 1px 4px oklch(0 0 0 / .28)}
 .manga-title{font-size:.84rem;font-weight:550;line-height:1.3;display:-webkit-box;-webkit-line-clamp:2;
              -webkit-box-orient:vertical;overflow:hidden}
 .global-group{margin-bottom:1.65rem}
 .global-head{display:flex;align-items:center;justify-content:space-between;gap:.75rem;margin-bottom:.75rem;
              padding-bottom:.45rem;border-bottom:1px solid var(--line)}
 .global-head h2{display:flex;align-items:center;gap:.45rem;font-size:.98rem}
 .pager{display:flex;justify-content:space-between;align-items:center;padding-top:.75rem;border-top:1px solid var(--line)}
 .ext-icon{width:32px;height:32px;border-radius:.4rem;object-fit:cover;vertical-align:middle;margin-right:.55rem;background:var(--raise);border:1px solid var(--line);flex-shrink:0}
 .ext-icon-fallback{width:32px;height:32px;border-radius:.4rem;display:inline-flex;align-items:center;justify-content:center;
                    font-size:.82rem;font-weight:650;color:var(--muted);background:var(--raise);border:1px solid var(--line);
                    margin-right:.55rem;flex-shrink:0}
 .ext-name{display:inline-flex;align-items:center;font-weight:550}
 .ext-row.hidden{display:none}
 .nsfw-toggle{display:inline-flex;align-items:center;gap:.35rem;font-size:.825rem;color:var(--muted);white-space:nowrap;cursor:pointer;padding:.25rem .4rem;user-select:none}
 .nsfw-toggle input{accent-color:var(--accent-solid);margin:0}
 .tag.warn{background:var(--warn-bg);color:var(--warn)}
 .tag.bad{background:var(--bad-bg);color:var(--bad)}
 .repo-list{list-style:none;margin:0 0 .85rem;padding:0}
 .repo-list li{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.5rem .75rem;
               padding:.48rem 0;border-bottom:1px solid var(--line)}
 .repo-list li:last-child{border-bottom:0}
 .repo-list code{word-break:break-all;font-size:.8rem}
 .repo-form{display:flex;flex-wrap:wrap;gap:.45rem;align-items:center}
 .apk-divider{border-top:1px solid var(--line);margin-top:.95rem;padding-top:.85rem}
 .apk-form{display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;justify-content:space-between}
 .apk-form input[type=file]{font:inherit;font-size:.825rem;color:var(--muted);max-width:20rem}
</style>
<body><div class="wrap">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

{{if not .Configured}}
<section class="empty">
  <h2>Suwayomi-Server is not connected</h2>
  <p>Configure your Suwayomi-Server URL in <a href="/settings#suwayomi">Settings</a> to browse sources, manage extensions, and read manga directly from Yatagarasu.</p>
</section>
{{else}}
<div class="tabs">
  <a href="/browse?tab=sources"{{if eq .Tab "sources"}} class="active"{{end}}>Sources</a>
  <a href="/browse?tab=extensions"{{if eq .Tab "extensions"}} class="active"{{end}}>Extensions</a>
</div>

{{if eq .Tab "sources"}}
  {{if .Sources}}
  <div class="toolbar">
    <div class="toolbar-left">
      <form method="get" action="/browse" class="inline" style="margin:0;display:inline-flex;gap:.4rem;align-items:center">
        <input type="hidden" name="tab" value="sources">
        <input type="hidden" name="type" value="{{.BrowseType}}">
        {{if .Query}}<input type="hidden" name="q" value="{{.Query}}">{{end}}
        <select name="source" class="source-select" aria-label="Source">
          <option value="all"{{if .GlobalMode}} selected{{end}}>All sources (Global Search)</option>
          {{range .Sources}}
          <option value="{{.ID}}"{{if eq .ID $.SourceID}} selected{{end}}>{{.Name}} ({{.Lang}})</option>
          {{end}}
        </select>
        <button class="btn sm" type="submit">Go</button>
      </form>
      {{if and (not .GlobalMode) .SelectedSource}}
      <a class="btn sm" href="/source/{{.SelectedSource.ID}}/settings">Source Settings</a>
      {{end}}
      <div class="seg">
        {{if not .GlobalMode}}
        <a href="/browse?tab=sources&source={{.SourceID}}&type=POPULAR"{{if eq .BrowseType "POPULAR"}} class="active"{{end}}>Popular</a>
        {{if and .SelectedSource .SelectedSource.SupportsLatest}}
        <a href="/browse?tab=sources&source={{.SourceID}}&type=LATEST"{{if eq .BrowseType "LATEST"}} class="active"{{end}}>Latest</a>
        {{end}}
        {{end}}
        <a href="/browse?tab=sources&source=all&type=SEARCH{{if .Query}}&q={{.Query}}{{end}}"{{if .GlobalMode}} class="active"{{end}}>Global Search</a>
      </div>
    </div>
    <form method="get" action="/browse" class="search-bar">
      <input type="hidden" name="tab" value="sources">
      <input type="hidden" name="source" value="{{.SourceID}}">
      <input type="hidden" name="type" value="SEARCH">
      <input name="q" value="{{.Query}}" placeholder="{{if .GlobalMode}}Search all installed sources...{{else if .SelectedSource}}Search {{.SelectedSource.Name}}...{{else}}Search source...{{end}}" aria-label="Search manga">
      <button class="btn sm primary" type="submit">Search</button>
    </form>
  </div>

  {{if .GlobalMode}}
    {{if eq .Query ""}}
    <section class="empty">
      <h2>Global Search</h2>
      <p>Enter a title in the search box above to search across all {{len .Sources}} installed sources at once.</p>
    </section>
    {{else if .GlobalResults}}
      {{range .GlobalResults}}
      <section class="global-group">
        <div class="global-head">
          <h2>{{.Source.Name}} <span class="tag muted">{{.Source.Lang}}</span></h2>
          <a class="btn ghost" href="/browse?tab=sources&source={{.Source.ID}}&type=SEARCH&q={{$.Query}}">Open in {{.Source.Name}} →</a>
        </div>
        <div class="manga-grid">
          {{range .Mangas}}
          <a class="manga-card" href="/manga/{{.ID}}">
            <div class="cover-wrap">
              {{if .ThumbnailURL}}<img src="{{.ThumbnailURL}}" alt="{{.Title}}" loading="lazy">{{end}}
              {{if .InLibrary}}<span class="cover-badge">In Library</span>{{end}}
            </div>
            <span class="manga-title">{{.Title}}</span>
          </a>
          {{end}}
        </div>
      </section>
      {{end}}
    {{else}}
    <section class="empty">
      <h2>No global results for “{{.Query}}”</h2>
      <p>None of the installed sources returned matches. Try a shorter query or install more extensions.</p>
    </section>
    {{end}}
  {{else if and .BrowseResult .BrowseResult.Mangas}}
  <div class="manga-grid">
    {{range .BrowseResult.Mangas}}
    <a class="manga-card" href="/manga/{{.ID}}">
      <div class="cover-wrap">
        {{if .ThumbnailURL}}<img src="{{.ThumbnailURL}}" alt="{{.Title}}" loading="lazy">{{end}}
        {{if .InLibrary}}<span class="cover-badge">In Library</span>{{end}}
      </div>
      <span class="manga-title">{{.Title}}</span>
    </a>
    {{end}}
  </div>
  <div class="pager">
    {{if gt .Page 1}}
    <a class="btn sm" href="/browse?tab=sources&source={{.SourceID}}&type={{.BrowseType}}&q={{.Query}}&page={{.PrevPage}}">← Previous</a>
    {{else}}<span></span>{{end}}
    <span class="hint">Page {{.Page}}</span>
    {{if .BrowseResult.HasNextPage}}
    <a class="btn sm" href="/browse?tab=sources&source={{.SourceID}}&type={{.BrowseType}}&q={{.Query}}&page={{.NextPage}}">Next →</a>
    {{else}}<span></span>{{end}}
  </div>
  {{else}}
  <section class="empty">
    <h2>No results</h2>
    <p>This source returned no titles for the current view. Try switching to Popular, Latest, or a different search query.</p>
  </section>
  {{end}}
  {{else}}
  <section class="empty">
    <h2>No sources installed yet</h2>
    <p>Install an extension in the <a href="/browse?tab=extensions">Extensions</a> tab to start browsing manga.</p>
  </section>
  {{end}}
{{else}}
  <section class="panel">
    <div class="panel-head">
      <h2>Extension Repositories <span class="pill">{{len .ExtRepos}}</span></h2>
      <form method="post" action="/browse/extensions" style="margin:0">
        <input type="hidden" name="action" value="refresh">
        <button class="btn sm" type="submit">Refresh extensions</button>
      </form>
    </div>
    {{if .ExtRepos}}
    <ul class="repo-list">
      {{range .ExtRepos}}
      <li>
        <code>{{.}}</code>
        <form class="inline" method="post" action="/browse/extensions" data-confirm="Remove extension repository {{.}}?">
          <input type="hidden" name="action" value="remove-repo">
          <input type="hidden" name="repoUrl" value="{{.}}">
          <button class="btn ghost" type="submit">Remove</button>
        </form>
      </li>
      {{end}}
    </ul>
    {{else}}
    <p class="hint" style="margin-bottom:.75rem">No Mihon / Keiyoushi extension repositories configured in Suwayomi yet. Add an <code>index.min.json</code> URL below.</p>
    {{end}}
    <form class="repo-form" method="post" action="/browse/extensions">
      <input type="hidden" name="action" value="add-repo">
      <input class="repo-input" name="repoUrl" type="url" required placeholder="https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json" aria-label="Extension repository URL">
      <button class="btn sm primary" type="submit">Add repo</button>
    </form>
    <div class="apk-divider">
      <form class="apk-form" method="post" action="/browse/extensions/install-apk" enctype="multipart/form-data">
        <span class="hint">Install external extension <code>.apk</code></span>
        <div style="display:flex;flex-wrap:wrap;gap:.45rem;align-items:center">
          <input type="file" name="file" accept=".apk" required aria-label="Extension APK file">
          <button class="btn sm" type="submit">Upload APK</button>
        </div>
      </form>
    </div>
  </section>

  <div class="toolbar">
    <div class="search-bar" style="max-width:34rem">
      <input id="extFilter" type="search" value="{{.ExtQuery}}" placeholder="Filter extensions by name or package..." aria-label="Filter extensions">
      <select id="extLangFilter" class="lang-select" aria-label="Filter by language">
        <option value="">All languages</option>
        {{range .ExtLangs}}
        <option value="{{.}}"{{if eq . $.ExtLang}} selected{{end}}>{{.}}</option>
        {{end}}
      </select>
      <label class="nsfw-toggle"><input type="checkbox" id="extNsfwToggle"> Show 18+</label>
    </div>
    <span class="hint">Instant filter across installed &amp; available extensions</span>
  </div>

  {{if .UpdateExts}}
  <section class="card-table">
    <header>
      <h2>Updates available <span class="pill" id="updCount">{{len .UpdateExts}}</span></h2>
      <form method="post" action="/browse/extensions" style="margin:0">
        <input type="hidden" name="action" value="update-all">
        <button class="btn sm primary" type="submit">Update All</button>
      </form>
    </header>
    <table>
      <thead><tr><th>Extension</th><th>Lang</th><th class="size">Version</th><th></th></tr></thead>
      <tbody>
        {{range .UpdateExts}}
        <tr class="ext-row" data-group="upd" data-name="{{.Name}}" data-lang="{{.Lang}}" data-pkg="{{.PkgName}}" data-nsfw="{{if .IsNsfw}}true{{else}}false{{end}}">
          <td>
            <span class="ext-name">
              {{if .IconURL}}<img class="ext-icon" src="{{.IconURL}}" alt="" loading="lazy">{{else}}<span class="ext-icon-fallback">{{.Initial}}</span>{{end}}
              {{.Name}}
            </span>
            {{if .IsNsfw}} <span class="tag bad">18+</span>{{end}}
            {{if .IsObsolete}} <span class="tag warn">obsolete</span>{{end}}
          </td>
          <td><span class="tag muted">{{.Lang}}</span></td>
          <td class="size">{{.VersionName}}</td>
          <td class="state">
            <form class="inline" method="post" action="/browse/extensions">
              <input type="hidden" name="action" value="update">
              <input type="hidden" name="pkgName" value="{{.PkgName}}">
              <button class="btn sm primary" type="submit">Update</button>
            </form>
            <form class="inline" method="post" action="/browse/extensions" data-confirm="Uninstall {{.Name}}?">
              <input type="hidden" name="action" value="uninstall">
              <input type="hidden" name="pkgName" value="{{.PkgName}}">
              <button class="btn ghost" type="submit">Uninstall</button>
            </form>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </section>
  {{end}}

  <section class="card-table">
    <header>
      <h2>Installed extensions</h2>
      <p><span id="instCount">{{len .InstalledExts}}</span> installed</p>
    </header>
    {{if .InstalledExts}}
    <table>
      <thead><tr><th>Extension</th><th>Lang</th><th class="size">Version</th><th></th></tr></thead>
      <tbody>
        {{range .InstalledExts}}
        <tr class="ext-row" data-group="inst" data-name="{{.Name}}" data-lang="{{.Lang}}" data-pkg="{{.PkgName}}" data-nsfw="{{if .IsNsfw}}true{{else}}false{{end}}">
          <td>
            <span class="ext-name">
              {{if .IconURL}}<img class="ext-icon" src="{{.IconURL}}" alt="" loading="lazy">{{else}}<span class="ext-icon-fallback">{{.Initial}}</span>{{end}}
              {{.Name}}
            </span>
            {{if .IsNsfw}} <span class="tag bad">18+</span>{{end}}
            {{if .IsObsolete}} <span class="tag warn">obsolete</span>{{end}}
          </td>
          <td><span class="tag muted">{{.Lang}}</span></td>
          <td class="size">{{.VersionName}}</td>
          <td class="state">
            <form class="inline" method="post" action="/browse/extensions" data-confirm="Uninstall {{.Name}}?">
              <input type="hidden" name="action" value="uninstall">
              <input type="hidden" name="pkgName" value="{{.PkgName}}">
              <button class="btn ghost" type="submit">Uninstall</button>
            </form>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <p class="hint" style="padding:1rem 1.25rem">No installed extensions match the current filter.</p>
    {{end}}
  </section>

  <section class="card-table">
    <header>
      <h2>Available extensions</h2>
      <p><span id="availCount">{{len .AvailableExts}}</span> available</p>
    </header>
    {{if .AvailableExts}}
    <table>
      <thead><tr><th>Extension</th><th>Lang</th><th class="size">Version</th><th></th></tr></thead>
      <tbody>
        {{range .AvailableExts}}
        <tr class="ext-row" data-group="avail" data-name="{{.Name}}" data-lang="{{.Lang}}" data-pkg="{{.PkgName}}" data-nsfw="{{if .IsNsfw}}true{{else}}false{{end}}">
          <td>
            <span class="ext-name">
              {{if .IconURL}}<img class="ext-icon" src="{{.IconURL}}" alt="" loading="lazy">{{else}}<span class="ext-icon-fallback">{{.Initial}}</span>{{end}}
              {{.Name}}
            </span>
            {{if .IsNsfw}} <span class="tag bad">18+</span>{{end}}
            {{if .IsObsolete}} <span class="tag warn">obsolete</span>{{end}}
          </td>
          <td><span class="tag muted">{{.Lang}}</span></td>
          <td class="size">{{.VersionName}}</td>
          <td class="state">
            <form class="inline" method="post" action="/browse/extensions">
              <input type="hidden" name="action" value="install">
              <input type="hidden" name="pkgName" value="{{.PkgName}}">
              <button class="btn sm" type="submit">Install</button>
            </form>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <p class="hint" style="padding:1rem 1.25rem">No available extensions found. Add an extension repo above and click Refresh extensions.</p>
    {{end}}
  </section>
{{end}}
{{end}}

</div>
<script nonce="{{.Nonce}}">
for (const f of document.querySelectorAll("form[data-confirm]"))
  f.addEventListener("submit", e => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });

(function(){
  const qEl = document.getElementById("extFilter");
  const lEl = document.getElementById("extLangFilter");
  const nsfwEl = document.getElementById("extNsfwToggle");
  if (!qEl && !lEl && !nsfwEl) return;
  const rows = Array.from(document.querySelectorAll(".ext-row"));
  const updCount = document.getElementById("updCount");
  const instCount = document.getElementById("instCount");
  const availCount = document.getElementById("availCount");
  try {
    if (nsfwEl && localStorage.getItem("yata_ext_show_nsfw") === "1") nsfwEl.checked = true;
  } catch (e) {}
  function applyFilter() {
    const q = qEl ? qEl.value.trim().toLowerCase() : "";
    const lang = lEl ? lEl.value.trim().toLowerCase() : "";
    const showNsfw = nsfwEl ? nsfwEl.checked : true;
    let updN = 0, instN = 0, availN = 0;
    rows.forEach(r => {
      const rl = (r.dataset.lang || "").toLowerCase();
      const hay = ((r.dataset.name || "") + " " + rl + " " + (r.dataset.pkg || "")).toLowerCase();
      const nsfwOk = showNsfw || r.dataset.nsfw !== "true";
      const ok = nsfwOk && (!q || hay.includes(q)) && (!lang || rl === lang);
      r.classList.toggle("hidden", !ok);
      if (ok) {
        if (r.dataset.group === "upd") updN++;
        else if (r.dataset.group === "inst") instN++;
        else availN++;
      }
    });
    if (updCount) updCount.textContent = String(updN);
    if (instCount) instCount.textContent = String(instN);
    if (availCount) availCount.textContent = String(availN);
  }
  if (qEl) qEl.addEventListener("input", applyFilter);
  if (lEl) lEl.addEventListener("change", applyFilter);
  if (nsfwEl) nsfwEl.addEventListener("change", () => {
    try { localStorage.setItem("yata_ext_show_nsfw", nsfwEl.checked ? "1" : "0"); } catch (e) {}
    applyFilter();
  });
  applyFilter();
})();
</script>
`))

var mangaTmpl = template.Must(template.New("manga").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Manga.Title}} — Yatagarasu</title>` + baseHead + `
<style>
 .hero{display:grid;grid-template-columns:1fr;gap:1.25rem;margin-bottom:1.5rem;
       background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:1.25rem}
 @media (min-width:44rem){.hero{grid-template-columns:12rem 1fr;gap:1.5rem}}
 .hero-cover{aspect-ratio:2/3;border-radius:var(--radius);overflow:hidden;background:var(--raise);
             border:1px solid var(--line);max-width:12rem}
 .hero-cover img{width:100%;height:100%;object-fit:cover;display:block}
 .hero-meta h1{font-size:1.5rem;margin-bottom:.35rem}
 .hero-sub{color:var(--muted);font-size:.875rem;margin:0 0 .65rem}
 .tags{display:flex;flex-wrap:wrap;gap:.35rem;margin-bottom:.8rem}
 .desc{font-size:.9rem;color:var(--ink);line-height:1.55;margin:0 0 1rem;white-space:pre-line}
 .hero-actions{display:flex;flex-wrap:wrap;gap:.55rem;align-items:center}
 .cat-editor{display:flex;flex-wrap:wrap;gap:.55rem .85rem;align-items:center}
 .cat-check{display:inline-flex;align-items:center;gap:.35rem;padding:.28rem .6rem;border-radius:999px;
            border:1px solid var(--line);background:var(--bg);font-size:.825rem;cursor:pointer;user-select:none}
 .cat-check input{accent-color:var(--accent-solid);margin:0}
 .cat-check:hover{border-color:var(--muted)}
 .ch-link{color:var(--ink);text-decoration:none;font-weight:530}
 .ch-link:hover{color:var(--accent)}
 .ch-read .ch-link{color:var(--muted);font-weight:450}
 .ch-sub{display:block;font-size:.75rem;color:var(--muted)}
</style>
<body><div class="wrap">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<section class="hero">
  <div class="hero-cover">
    {{if .Manga.ThumbnailURL}}<img src="{{.Manga.ThumbnailURL}}" alt="{{.Manga.Title}}">{{end}}
  </div>
  <div class="hero-meta">
    <h1>{{.Manga.Title}}</h1>
    <p class="hero-sub">
      {{if .Manga.Author}}{{.Manga.Author}}{{end}}{{if and .Manga.Artist (ne .Manga.Artist .Manga.Author)}} · {{.Manga.Artist}}{{end}}
      {{if .Manga.SourceName}}{{if .Manga.Author}} · {{end}}{{.Manga.SourceName}}{{if .Manga.SourceLang}} ({{.Manga.SourceLang}}){{end}}{{end}}
    </p>
    <div class="tags">
      {{if .Manga.Status}}<span class="tag accent">{{.Manga.Status}}</span>{{end}}
      {{if .Manga.InLibrary}}<span class="tag">In Library</span>{{end}}
      {{range .Manga.Categories}}<span class="tag muted">{{.}}</span>{{end}}
      {{range .Manga.Genres}}<span class="tag muted">{{.}}</span>{{end}}
    </div>
    {{if .Manga.Description}}<p class="desc">{{.Manga.Description}}</p>{{end}}
    <div class="hero-actions">
      <form method="post" action="/manga/{{.Manga.ID}}/library">
        {{if .Manga.InLibrary}}
        <input type="hidden" name="inLibrary" value="false">
        <button class="btn" type="submit">Remove from Library</button>
        {{else}}
        <input type="hidden" name="inLibrary" value="true">
        <button class="btn primary" type="submit">Add to Library</button>
        {{end}}
      </form>
      <form method="post" action="/manga/{{.Manga.ID}}/refresh">
        <button class="btn" type="submit">Refresh Chapters</button>
      </form>
      <a class="btn" href="/manga/{{.Manga.ID}}/migrate">Migrate</a>
      {{if .Manga.SourceID}}
      <a class="btn ghost" href="/browse?tab=sources&source={{.Manga.SourceID}}">← Back to source</a>
      {{end}}
    </div>
  </div>
</section>

{{if .CategoryChoices}}
<section class="panel">
  <div class="panel-head">
    <h2>Categories</h2>
    <a class="btn ghost" href="/categories">Manage categories</a>
  </div>
  <form class="cat-editor" method="post" action="/manga/{{.Manga.ID}}/categories">
    {{range .CategoryChoices}}
    <label class="cat-check">
      <input type="checkbox" name="categoryIds" value="{{.ID}}"{{if .Checked}} checked{{end}}>
      <span>{{.Name}}</span>
    </label>
    {{end}}
    <button class="btn sm primary" type="submit">Save Categories</button>
  </form>
</section>
{{end}}

<section class="card-table">
  <header>
    <h2>Chapters</h2>
    <p>{{.TotalCount}} total · {{.ReadCount}} read · {{.DlCount}} downloaded</p>
  </header>
  {{if .Chapters}}
  <table>
    <thead><tr><th>#</th><th>Chapter</th><th class="size">Pages</th><th>Uploaded</th><th></th></tr></thead>
    <tbody>
      {{range .Chapters}}
      <tr{{if .IsRead}} class="ch-read"{{end}}>
        <td class="num">{{.Number}}</td>
        <td>
          <a class="ch-link" href="/read/{{.ID}}">{{.Name}}</a>
          {{if .Scanlator}}<span class="ch-sub">{{.Scanlator}}</span>{{end}}
        </td>
        <td class="size">{{if gt .PageCount 0}}{{.PageCount}}{{else}}—{{end}}</td>
        <td class="when">{{.Uploaded}}</td>
        <td class="state">
          <a class="btn sm primary" href="/read/{{.ID}}">Read</a>
          {{if .IsDownloaded}}
          <span class="tag">saved</span>
          <form class="inline" method="post" action="/manga/{{$.Manga.ID}}/chapter/{{.ID}}">
            <input type="hidden" name="action" value="delete-download">
            <button class="btn ghost" type="submit" title="Delete downloaded files">delete</button>
          </form>
          {{else}}
          <form class="inline" method="post" action="/manga/{{$.Manga.ID}}/chapter/{{.ID}}">
            <input type="hidden" name="action" value="download">
            <button class="btn sm" type="submit">Download</button>
          </form>
          {{end}}
          <form class="inline" method="post" action="/manga/{{$.Manga.ID}}/chapter/{{.ID}}">
            {{if .IsRead}}
            <input type="hidden" name="action" value="unread">
            <button class="btn ghost" type="submit">unread</button>
            {{else}}
            <input type="hidden" name="action" value="read">
            <button class="btn ghost" type="submit">mark read</button>
            {{end}}
          </form>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="hint" style="padding:1rem 1.25rem">No chapters fetched yet. Click <strong>Refresh Chapters</strong> above.</p>
  {{end}}
</section>

</div>
`))

var downloadsTmpl = template.Must(template.New("downloads").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu — downloads</title>` + baseHead + `
<style>
 .dl-controls{display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;justify-content:space-between}
 .dl-btns{display:flex;flex-wrap:wrap;gap:.45rem;align-items:center}
 .prog{display:flex;align-items:center;gap:.55rem;min-width:8rem}
 .bar-bg{flex:1;height:5px;background:var(--line);border-radius:999px;overflow:hidden}
 .bar-fg{height:100%;background:var(--accent-solid);border-radius:999px}
 .prog span{font-size:.78rem;color:var(--muted);font-variant-numeric:tabular-nums;white-space:nowrap}
 .mini-cover{width:28px;height:40px;border-radius:.25rem;object-fit:cover;vertical-align:middle;margin-right:.55rem;background:var(--raise)}
 .dl-manga{display:inline-flex;align-items:center;text-decoration:none;color:var(--ink);font-weight:550}
 .dl-manga:hover{color:var(--accent)}
</style>
<body><div class="wrap">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

{{if not .Configured}}
<section class="empty">
  <h2>Suwayomi-Server is not connected</h2>
  <p>Configure your Suwayomi-Server URL in <a href="/settings#suwayomi">Settings</a> to manage background chapter downloads and library updates.</p>
</section>
{{else}}
<section class="panel">
  <div class="dl-controls">
    <div>
      <div style="display:flex;align-items:center;gap:.6rem;margin-bottom:.2rem">
        <h2>Suwayomi Downloader</h2>
        {{if eq .State "STARTED"}}<span class="dot ok">{{.State}}</span>{{else}}<span class="dot off">{{.State}}</span>{{end}}
      </div>
      <p class="hint">{{len .Items}} {{if eq (len .Items) 1}}chapter{{else}}chapters{{end}} in queue</p>
    </div>
    <div class="dl-btns">
      {{if eq .State "STARTED"}}
      <form method="post" action="/downloads/control">
        <input type="hidden" name="action" value="stop">
        <button class="btn" type="submit">Pause</button>
      </form>
      {{else}}
      <form method="post" action="/downloads/control">
        <input type="hidden" name="action" value="start">
        <button class="btn primary" type="submit">Resume</button>
      </form>
      {{end}}
      <form method="post" action="/downloads/control" data-confirm="Clear all items from the download queue?">
        <input type="hidden" name="action" value="clear">
        <button class="btn" type="submit">Clear Queue</button>
      </form>
      <form method="post" action="/downloads/control">
        <input type="hidden" name="action" value="update-library">
        <button class="btn" type="submit">Update Library</button>
      </form>
    </div>
  </div>
</section>

{{if .Items}}
<section class="card-table">
  <header>
    <h2>Download queue</h2>
    <p>{{len .Items}} queued</p>
  </header>
  <table>
    <thead><tr><th>Manga</th><th>Chapter</th><th>State</th><th>Progress</th><th></th></tr></thead>
    <tbody>
      {{range .Items}}
      <tr>
        <td>
          <a class="dl-manga" href="/manga/{{.MangaID}}">
            {{if .ThumbnailURL}}<img class="mini-cover" src="{{.ThumbnailURL}}" alt="" loading="lazy">{{end}}
            {{.MangaTitle}}
          </a>
        </td>
        <td>{{.ChapterName}}</td>
        <td><span class="tag {{if eq .State "DOWNLOADING"}}accent{{else if eq .State "ERROR"}}muted{{else}}muted{{end}}">{{.State}}</span></td>
        <td>
          <div class="prog">
            <div class="bar-bg"><div class="bar-fg" style="width:{{.ProgressPct}}%"></div></div>
            <span>{{.ProgressPct}}%</span>
          </div>
        </td>
        <td class="state">
          <form class="inline" method="post" action="/downloads/control">
            <input type="hidden" name="action" value="dequeue">
            <input type="hidden" name="chapterId" value="{{.ChapterID}}">
            <button class="btn ghost" type="submit">remove</button>
          </form>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
</section>
{{else}}
<section class="empty">
  <h2>Download queue is empty</h2>
  <p>Queue chapters from any <a href="/browse">Manga details</a> page or click <strong>Update Library</strong> to check for new releases.</p>
</section>
{{end}}
{{end}}

</div>
<script nonce="{{.Nonce}}">
for (const f of document.querySelectorAll("form[data-confirm]"))
  f.addEventListener("submit", e => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });
</script>
`))

var readerTmpl = template.Must(template.New("reader").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.ChapterTitle}} · {{.MangaTitle}} — Yatagarasu</title>` + favicon + `
<style>
 :root{
   --bg:oklch(0.14 0.012 50); --bar:oklch(0.165 0.014 50 / .92); --drawer:oklch(0.185 0.014 50);
   --surface:oklch(0.22 0.014 50); --line:oklch(0.30 0.014 50);
   --ink:oklch(0.94 0.008 60); --muted:oklch(0.70 0.012 60);
   --accent:oklch(0.76 0.145 52); --accent-solid:oklch(0.70 0.155 48); --accent-soft:oklch(0.28 0.055 48);
   --on-accent:oklch(0.18 0.025 50);
   --ok:oklch(0.78 0.11 160); --ok-bg:oklch(0.26 0.05 160);
   --radius:.48rem; --ease:cubic-bezier(.22,1,.36,1);
 }
 body.bg-black{--bg:#0a0a0a}
 body.bg-ink{--bg:oklch(0.14 0.012 50)}
 body.bg-paper{
   --bg:oklch(0.955 0.012 65); --bar:oklch(0.985 0.006 60 / .94); --drawer:oklch(0.99 0.004 60);
   --surface:oklch(0.95 0.008 60); --line:oklch(0.86 0.012 58);
   --ink:oklch(0.22 0.020 48); --muted:oklch(0.47 0.018 50);
   --accent:oklch(0.52 0.170 40); --accent-solid:oklch(0.50 0.170 38); --accent-soft:oklch(0.93 0.038 45);
   --on-accent:oklch(0.99 0.01 55);
   --ok:oklch(0.42 0.105 155); --ok-bg:oklch(0.92 0.04 155);
 }
 *{box-sizing:border-box}
 html,body{margin:0;padding:0;background:var(--bg);color:var(--ink);
   font:14px/1.45 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;min-height:100vh}

 /* Floating auto-hiding Top & Bottom HUD */
 .rbar-top,.rbar-bot{
   position:fixed;left:0;right:0;z-index:30;display:flex;align-items:center;justify-content:space-between;
   gap:.55rem;padding:.55rem .95rem;background:var(--bar);backdrop-filter:blur(10px);
   transition:transform 180ms var(--ease),opacity 180ms var(--ease);
 }
 .rbar-top{top:0;border-bottom:1px solid var(--line)}
 .rbar-bot{bottom:0;border-top:1px solid var(--line)}
 body.hud-hidden .rbar-top{transform:translateY(-100%);opacity:0;pointer-events:none}
 body.hud-hidden .rbar-bot{transform:translateY(100%);opacity:0;pointer-events:none}

 .rbar-left,.rbar-right{display:flex;align-items:center;gap:.45rem;min-width:0}
 .rtitle{min-width:0;max-width:24rem}
 .rtitle strong{display:block;font-size:.875rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
 .rtitle span{display:block;font-size:.74rem;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}

 .rbtn{
   display:inline-flex;align-items:center;gap:.35rem;padding:.34rem .62rem;border-radius:var(--radius);
   border:1px solid var(--line);background:transparent;color:var(--ink);font:inherit;font-size:.8rem;
   font-weight:540;text-decoration:none;cursor:pointer;white-space:nowrap;
   transition:background 130ms var(--ease),border-color 130ms var(--ease);
 }
 .rbtn:hover{border-color:var(--muted);background:var(--surface)}
 .rbtn.active{background:var(--accent-soft);border-color:var(--accent);color:var(--accent);font-weight:600}
 .rbtn.done{background:var(--ok-bg);border-color:transparent;color:var(--ok)}
 .rbtn.disabled{opacity:.36;pointer-events:none}

 /* Bottom Scrubber */
 .scrubber{display:flex;align-items:center;gap:.65rem;flex:1;max-width:44rem;margin:0 auto}
 .scrubber input[type=range]{flex:1;min-width:6rem;accent-color:var(--accent-solid);cursor:pointer;height:5px}
 .rcounter{font-size:.8rem;font-variant-numeric:tabular-nums;color:var(--muted);white-space:nowrap;min-width:5.8rem;text-align:center}

 /* Stage & Reading Modes */
 .stage{width:100%;min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;user-select:none}
 .stage img{display:none;margin:0;user-select:none;-webkit-user-drag:none}

 /* Webtoon (Vertical) */
 .stage.mode-webtoon{justify-content:flex-start;padding:0 0 4.5rem}
 .stage.mode-webtoon img{display:block;cursor:pointer}
 .stage.mode-webtoon.fit-width img{width:100%;max-width:56rem;height:auto}
 .stage.mode-webtoon.fit-screen img{max-width:100vw;max-height:100vh;width:auto;height:auto;object-fit:contain}
 .stage.mode-webtoon.fit-height img{height:100vh;width:auto;max-width:100vw;object-fit:contain}
 .stage.mode-webtoon.fit-original img{width:auto;height:auto;max-width:none}

 /* Single Page (RTL / LTR) */
 .stage.mode-single{flex-direction:row;align-items:center;justify-content:center;padding:0;min-height:100vh}
 .stage.mode-single img.current{display:block;cursor:pointer}
 .stage.mode-single.fit-screen img.current{max-width:100vw;max-height:100vh;width:auto;height:auto;object-fit:contain}
 .stage.mode-single.fit-width img.current{width:100vw;height:auto;max-height:none}
 .stage.mode-single.fit-height img.current{height:100vh;width:auto;max-width:none}
 .stage.mode-single.fit-original img.current{width:auto;height:auto;max-width:none}

 /* Double Page (RTL / LTR) */
 .stage.mode-double{flex-direction:row;align-items:center;justify-content:center;gap:0;padding:0;min-height:100vh}
 .stage.mode-double.dir-rtl{flex-direction:row-reverse}
 .stage.mode-double img.current{display:block;cursor:pointer}
 .stage.mode-double.fit-screen img.current{max-width:50vw;max-height:100vh;width:auto;height:auto;object-fit:contain}
 .stage.mode-double.fit-width img.current{width:50vw;height:auto;max-height:none}
 .stage.mode-double.fit-height img.current{height:100vh;width:auto;max-width:50vw;object-fit:contain}
 .stage.mode-double.fit-original img.current{width:auto;height:auto;max-width:none}

 .rfoot{display:flex;gap:.75rem;justify-content:center;align-items:center;padding:2rem 1rem 5rem}
 body:not(.is-webtoon) .rfoot{display:none}
 .empty-reader{padding:6rem 1.5rem;text-align:center;color:var(--muted)}

 /* Slide-out Reader Settings Drawer */
 .rbackdrop{position:fixed;inset:0;z-index:40;background:oklch(0 0 0 / .48);opacity:0;pointer-events:none;transition:opacity 180ms var(--ease)}
 .rdrawer{
   position:fixed;top:0;right:0;bottom:0;z-index:45;width:min(21rem,90vw);background:var(--drawer);
   border-left:1px solid var(--line);padding:1rem 1.15rem 2rem;overflow-y:auto;
   transform:translateX(100%);transition:transform 200ms var(--ease);
 }
 body.drawer-open .rbackdrop{opacity:1;pointer-events:auto}
 body.drawer-open .rdrawer{transform:translateX(0)}
 .drawer-head{display:flex;align-items:center;justify-content:space-between;padding-bottom:.75rem;border-bottom:1px solid var(--line);margin-bottom:1rem}
 .drawer-head h2{margin:0;font-size:1rem;font-weight:620}
 .dgroup{margin-bottom:1.15rem}
 .dgroup h3{margin:0 0 .5rem;font-size:.76rem;font-weight:600;text-transform:uppercase;letter-spacing:.04em;color:var(--muted)}
 .dopts{display:flex;flex-direction:column;gap:.35rem}
 .dopts.row{flex-direction:row;flex-wrap:wrap}
 .dopt{
   display:flex;align-items:center;justify-content:space-between;padding:.48rem .7rem;border-radius:var(--radius);
   border:1px solid var(--line);background:transparent;color:var(--ink);font:inherit;font-size:.835rem;
   text-align:left;cursor:pointer;transition:background 130ms var(--ease),border-color 130ms var(--ease);
 }
 .dopts.row .dopt{flex:1;justify-content:center;min-width:5.4rem}
 .dopt:hover{background:var(--surface);border-color:var(--muted)}
 .dopt.active{background:var(--accent-soft);border-color:var(--accent);color:var(--accent);font-weight:600}
 .kbd-list{list-style:none;margin:0;padding:0;font-size:.78rem;color:var(--muted)}
 .kbd-list li{display:flex;justify-content:space-between;padding:.28rem 0;border-bottom:1px solid var(--line)}
 .kbd-list li:last-child{border-bottom:0}
 kbd{font:0.76rem/1.3 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;padding:.06rem .35rem;
     border:1px solid var(--line);border-radius:.28rem;background:var(--surface);color:var(--ink)}

 /* Translation status toast */
 .ttoast{
   position:fixed;top:3.8rem;left:50%;transform:translateX(-50%);z-index:35;
   display:none;align-items:center;gap:.5rem;padding:.45rem .85rem;border-radius:999px;
   background:var(--bar);border:1px solid var(--accent);color:var(--ink);font-size:.8rem;
   box-shadow:0 4px 18px oklch(0 0 0 / .35);backdrop-filter:blur(8px);
 }
 .ttoast.show{display:inline-flex}
 .spinner{width:12px;height:12px;border:2px solid var(--line);border-top-color:var(--accent);border-radius:50%;animation:spin .7s linear infinite}
 @keyframes spin{to{transform:rotate(360deg)}}
 @media (max-width:40rem){.rtitle{max-width:10rem}.rbtn .lbl-sm{display:none}}
</style>
<body class="bg-ink is-webtoon">

<header class="rbar-top" id="rbarTop">
  <div class="rbar-left">
    <a class="rbtn" href="{{.BackURL}}">← {{.BackLabel}}</a>
    <div class="rtitle">
      <strong>{{.ChapterTitle}}</strong>
      <span>{{.MangaTitle}}</span>
    </div>
  </div>
  <div class="rbar-right">
    {{if .HasTranslator}}
    <button class="rbtn" type="button" id="translateBtn" title="Toggle live page translation (T)" data-lang="{{.TranslatorLang}}">Translate</button>
    {{end}}
    <button class="rbtn{{if .IsRead}} done{{end}}" type="button" id="markReadBtn">{{if .IsRead}}Read ✓{{else}}Mark Read{{end}}</button>
    <button class="rbtn" type="button" id="settingsBtn" title="Reader settings (S)" aria-label="Reader settings">⚙ <span class="lbl-sm">Settings</span></button>
  </div>
</header>

<div class="ttoast" id="ttoast" role="status" aria-live="polite">
  <span class="spinner" id="tspinner"></span>
  <span id="ttoastMsg">Translating page…</span>
</div>

{{if .Pages}}
<main class="stage mode-webtoon fit-screen" id="stage" data-mark-url="{{.MarkReadURL}}" data-read="{{if .IsRead}}true{{else}}false{{end}}">
  {{range $i, $src := .Pages}}
  <img src="{{$src}}" data-orig-src="{{$src}}" alt="Page {{$i}}" data-idx="{{$i}}" loading="lazy">
  {{end}}
</main>
<footer class="rfoot">
  {{if .PrevURL}}<a class="rbtn" href="{{.PrevURL}}">← Previous Chapter</a>{{end}}
  <a class="rbtn" href="{{.BackURL}}">Back to {{.BackLabel}}</a>
  {{if .NextURL}}<a class="rbtn" href="{{.NextURL}}">Next Chapter →</a>{{end}}
</footer>
{{else}}
<div class="empty-reader">
  <p>No pages found for this chapter.</p>
  <p><a class="rbtn" href="{{.BackURL}}">← Back to {{.BackLabel}}</a></p>
</div>
{{end}}

<nav class="rbar-bot" id="rbarBot" aria-label="Page navigation">
  {{if .PrevURL}}<a class="rbtn" id="prevCh" href="{{.PrevURL}}">← <span class="lbl-sm">Prev Ch</span></a>{{else}}<span class="rbtn disabled">← <span class="lbl-sm">Prev Ch</span></span>{{end}}
  <div class="scrubber">
    <span class="rcounter" id="counter">Page 1 / {{.PageCount}}</span>
    <input type="range" id="pageSlider" min="1" max="{{if gt .PageCount 0}}{{.PageCount}}{{else}}1{{end}}" value="1" aria-label="Page scrubber">
  </div>
  {{if .NextURL}}<a class="rbtn" id="nextCh" href="{{.NextURL}}"><span class="lbl-sm">Next Ch</span> →</a>{{else}}<span class="rbtn disabled"><span class="lbl-sm">Next Ch</span> →</span>{{end}}
</nav>

<div class="rbackdrop" id="rbackdrop"></div>
<aside class="rdrawer" id="rdrawer" aria-label="Reader Settings">
  <div class="drawer-head">
    <h2>Reader Settings</h2>
    <button class="rbtn" type="button" id="closeDrawerBtn" aria-label="Close settings">✕</button>
  </div>

  <div class="dgroup">
    <h3>Reading Mode</h3>
    <div class="dopts">
      <button class="dopt" type="button" data-mode="webtoon">Webtoon (Vertical)</button>
      <button class="dopt" type="button" data-mode="single-rtl">Single Page (RTL)</button>
      <button class="dopt" type="button" data-mode="single-ltr">Single Page (LTR)</button>
      <button class="dopt" type="button" data-mode="double-rtl">Double Page (RTL)</button>
      <button class="dopt" type="button" data-mode="double-ltr">Double Page (LTR)</button>
    </div>
  </div>

  <div class="dgroup">
    <h3>Image Fit</h3>
    <div class="dopts row">
      <button class="dopt" type="button" data-fit="fit-width">Fit Width</button>
      <button class="dopt" type="button" data-fit="fit-height">Fit Height</button>
      <button class="dopt" type="button" data-fit="fit-screen">Fit Screen</button>
      <button class="dopt" type="button" data-fit="fit-original">Original</button>
    </div>
  </div>

  <div class="dgroup">
    <h3>Background</h3>
    <div class="dopts row">
      <button class="dopt" type="button" data-bg="black">Pitch Black</button>
      <button class="dopt" type="button" data-bg="ink">Dark Ink</button>
      <button class="dopt" type="button" data-bg="paper">Warm Paper</button>
    </div>
  </div>

  <div class="dgroup">
    <h3>Shortcuts</h3>
    <ul class="kbd-list">
      <li><span>Prev / Next Page</span><span><kbd>←</kbd> <kbd>→</kbd></span></li>
      <li><span>Advance Page</span><kbd>Space</kbd></li>
      <li><span>Toggle Bars</span><kbd>F</kbd></li>
      <li><span>Settings Drawer</span><kbd>S</kbd></li>
      {{if .HasTranslator}}<li><span>Translate Page</span><kbd>T</kbd></li>{{end}}
    </ul>
  </div>
</aside>

<script nonce="{{.Nonce}}">
(function(){
  const stage = document.getElementById("stage");
  if (!stage) return;
  const imgs = Array.from(stage.querySelectorAll("img"));
  const total = imgs.length;
  const counter = document.getElementById("counter");
  const slider = document.getElementById("pageSlider");
  const markBtn = document.getElementById("markReadBtn");
  const settingsBtn = document.getElementById("settingsBtn");
  const closeDrawerBtn = document.getElementById("closeDrawerBtn");
  const backdrop = document.getElementById("rbackdrop");
  const translateBtn = document.getElementById("translateBtn");
  const ttoast = document.getElementById("ttoast");
  const tspinner = document.getElementById("tspinner");
  const ttoastMsg = document.getElementById("ttoastMsg");

  const modeBtns = Array.from(document.querySelectorAll("[data-mode]"));
  const fitBtns = Array.from(document.querySelectorAll("[data-fit]"));
  const bgBtns = Array.from(document.querySelectorAll("[data-bg]"));

  const markURL = stage.dataset.markUrl;
  let isRead = stage.dataset.read === "true";
  let autoMarked = isRead;
  let idx = 0;

  // Restore persisted settings (with migration from legacy rtl/ltr values).
  let mode = localStorage.getItem("yata_reader_mode") || "webtoon";
  if (mode === "rtl") mode = "single-rtl";
  if (mode === "ltr") mode = "single-ltr";
  if (!["webtoon", "single-rtl", "single-ltr", "double-rtl", "double-ltr"].includes(mode)) mode = "webtoon";

  let fit = localStorage.getItem("yata_reader_fit") || "fit-screen";
  if (!["fit-width", "fit-height", "fit-screen", "fit-original"].includes(fit)) fit = "fit-screen";

  let bg = localStorage.getItem("yata_reader_bg") || "ink";
  if (!["black", "ink", "paper"].includes(bg)) bg = "ink";

  // Live translation state
  let translateOn = false;
  const translatedURLs = new Map();
  const pendingLoads = new Set();
  let toastTimer = null;

  function isRTL() { return mode === "single-rtl" || mode === "double-rtl"; }
  function isDouble() { return mode === "double-rtl" || mode === "double-ltr"; }
  function stepSize() { return isDouble() ? 2 : 1; }

  function visibleIndices() {
    if (total === 0) return [];
    if (isDouble()) {
      return idx + 1 < total ? [idx, idx + 1] : [idx];
    }
    return [idx];
  }

  function updateCounter() {
    if (total === 0) return;
    if (counter) {
      if (isDouble() && idx + 1 < total) {
        counter.textContent = "Page " + (idx + 1) + "–" + (idx + 2) + " / " + total;
      } else {
        counter.textContent = "Page " + (idx + 1) + " / " + total;
      }
    }
    if (slider) slider.value = String(idx + 1);
  }

  function sendMarkRead(want, pageIdx) {
    if (!markURL) return;
    fetch(markURL, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({read: want, lastPageRead: pageIdx})
    }).then(r => {
      if (r.ok) {
        isRead = want;
        if (want) autoMarked = true;
        if (markBtn) {
          markBtn.textContent = isRead ? "Read ✓" : "Mark Read";
          markBtn.classList.toggle("done", isRead);
        }
      }
    }).catch(() => {});
  }

  function maybeAutoMark() {
    const lastVisible = isDouble() ? Math.min(idx + 1, total - 1) : idx;
    if (!autoMarked && total > 0 && lastVisible >= total - 1) {
      autoMarked = true;
      sendMarkRead(true, lastVisible);
    }
  }

  function showToast(msg, spinning, autoHideMs) {
    if (!ttoast || !ttoastMsg) return;
    clearTimeout(toastTimer);
    ttoastMsg.textContent = msg;
    if (tspinner) tspinner.style.display = spinning ? "inline-block" : "none";
    ttoast.classList.add("show");
    if (autoHideMs > 0) {
      toastTimer = setTimeout(() => ttoast.classList.remove("show"), autoHideMs);
    }
  }

  function hideToast() {
    clearTimeout(toastTimer);
    if (ttoast) ttoast.classList.remove("show");
  }

  function translatePageIndex(pageIdx) {
    if (!translateBtn || !translateOn) return;
    const im = imgs[pageIdx];
    if (!im) return;
    const orig = im.dataset.origSrc;
    if (!orig) return;
    if (translatedURLs.has(pageIdx)) {
      im.src = translatedURLs.get(pageIdx);
      return;
    }
    if (pendingLoads.has(pageIdx)) return;
    pendingLoads.add(pageIdx);
    const lang = translateBtn.dataset.lang || "";
    const proxyURL = "/api/translations/proxy-page?src=" + encodeURIComponent(orig) + (lang ? "&lang=" + encodeURIComponent(lang) : "");
    showToast("Translating page " + (pageIdx + 1) + "…", true, 0);

    const pre = new Image();
    pre.onload = () => {
      pendingLoads.delete(pageIdx);
      translatedURLs.set(pageIdx, proxyURL);
      if (translateOn) im.src = proxyURL;
      if (pendingLoads.size === 0) hideToast();
    };
    pre.onerror = () => {
      pendingLoads.delete(pageIdx);
      showToast("Translation unavailable for page " + (pageIdx + 1), false, 2600);
    };
    pre.src = proxyURL;
  }

  function applyTranslationState() {
    if (!translateBtn) return;
    translateBtn.classList.toggle("active", translateOn);
    translateBtn.textContent = translateOn ? "Translated ✓" : "Translate";
    if (!translateOn) {
      hideToast();
      imgs.forEach(im => { if (im.dataset.origSrc) im.src = im.dataset.origSrc; });
      return;
    }
    visibleIndices().forEach(i => translatePageIndex(i));
  }

  function toggleTranslate() {
    if (!translateBtn) return;
    translateOn = !translateOn;
    applyTranslationState();
  }

  function showPage(nextIdx, scrollInWebtoon) {
    if (total === 0) return;
    if (nextIdx < 0) nextIdx = 0;
    if (nextIdx >= total) nextIdx = total - 1;
    if (isDouble()) {
      nextIdx = nextIdx - (nextIdx % 2);
    }
    idx = nextIdx;
    const vis = new Set(visibleIndices());
    imgs.forEach((im, i) => im.classList.toggle("current", vis.has(i)));
    updateCounter();
    maybeAutoMark();
    if (translateOn) visibleIndices().forEach(i => translatePageIndex(i));
    if (mode !== "webtoon") {
      window.scrollTo({top: 0});
    } else if (scrollInWebtoon && imgs[idx]) {
      imgs[idx].scrollIntoView({block: "start"});
    }
  }

  function setMode(m) {
    mode = m;
    try { localStorage.setItem("yata_reader_mode", mode); } catch (e) {}
    stage.classList.remove("mode-webtoon", "mode-single", "mode-double", "dir-rtl", "dir-ltr");
    if (mode === "webtoon") {
      stage.classList.add("mode-webtoon");
    } else if (isDouble()) {
      stage.classList.add("mode-double", isRTL() ? "dir-rtl" : "dir-ltr");
    } else {
      stage.classList.add("mode-single", isRTL() ? "dir-rtl" : "dir-ltr");
    }
    document.body.classList.toggle("is-webtoon", mode === "webtoon");
    modeBtns.forEach(b => b.classList.toggle("active", b.dataset.mode === mode));
    showPage(idx, mode === "webtoon");
  }

  function setFit(f) {
    fit = f;
    try { localStorage.setItem("yata_reader_fit", fit); } catch (e) {}
    stage.classList.remove("fit-width", "fit-height", "fit-screen", "fit-original");
    stage.classList.add(fit);
    fitBtns.forEach(b => b.classList.toggle("active", b.dataset.fit === fit));
  }

  function setBg(b) {
    bg = b;
    try { localStorage.setItem("yata_reader_bg", bg); } catch (e) {}
    document.body.classList.remove("bg-black", "bg-ink", "bg-paper");
    document.body.classList.add("bg-" + bg);
    bgBtns.forEach(btn => btn.classList.toggle("active", btn.dataset.bg === bg));
  }

  function toggleDrawer(force) {
    if (typeof force === "boolean") document.body.classList.toggle("drawer-open", force);
    else document.body.classList.toggle("drawer-open");
  }

  function toggleHUD() {
    document.body.classList.toggle("hud-hidden");
  }

  modeBtns.forEach(b => b.addEventListener("click", () => setMode(b.dataset.mode)));
  fitBtns.forEach(b => b.addEventListener("click", () => setFit(b.dataset.fit)));
  bgBtns.forEach(b => b.addEventListener("click", () => setBg(b.dataset.bg)));
  if (markBtn) markBtn.addEventListener("click", () => sendMarkRead(!isRead, idx));
  if (translateBtn) translateBtn.addEventListener("click", toggleTranslate);
  if (settingsBtn) settingsBtn.addEventListener("click", () => toggleDrawer());
  if (closeDrawerBtn) closeDrawerBtn.addEventListener("click", () => toggleDrawer(false));
  if (backdrop) backdrop.addEventListener("click", () => toggleDrawer(false));

  if (slider) {
    slider.addEventListener("input", () => {
      const target = parseInt(slider.value, 10) - 1;
      if (!Number.isNaN(target)) showPage(target, true);
    });
  }

  // Tap/click zones: center 40% toggles HUD; left 30% / right 30% navigates in paged modes.
  stage.addEventListener("click", e => {
    const x = e.clientX / Math.max(window.innerWidth, 1);
    if (mode === "webtoon") {
      if (x >= 0.3 && x <= 0.7) toggleHUD();
      else toggleHUD();
      return;
    }
    const step = stepSize();
    if (x < 0.3) {
      showPage(isRTL() ? idx + step : idx - step, false);
    } else if (x > 0.7) {
      showPage(isRTL() ? idx - step : idx + step, false);
    } else {
      toggleHUD();
    }
  });

  // Track visible page while scrolling in Webtoon mode
  if ("IntersectionObserver" in window) {
    const obs = new IntersectionObserver(entries => {
      if (mode !== "webtoon") return;
      for (const en of entries) {
        if (en.isIntersecting) {
          const i = parseInt(en.target.dataset.idx, 10);
          if (!Number.isNaN(i)) {
            idx = i;
            updateCounter();
            maybeAutoMark();
            if (translateOn) translatePageIndex(i);
          }
        }
      }
    }, {threshold: 0.35});
    imgs.forEach(im => obs.observe(im));
  }

  document.addEventListener("keydown", e => {
    if (e.target && (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA" || e.target.tagName === "SELECT")) return;
    if (e.key === "Escape") {
      toggleDrawer(false);
    } else if (e.key === "f" || e.key === "F") {
      e.preventDefault();
      toggleHUD();
    } else if (e.key === "s" || e.key === "S") {
      e.preventDefault();
      toggleDrawer();
    } else if ((e.key === "t" || e.key === "T") && translateBtn) {
      e.preventDefault();
      toggleTranslate();
    } else if (e.key === "ArrowLeft") {
      if (mode !== "webtoon") {
        e.preventDefault();
        showPage(isRTL() ? idx + stepSize() : idx - stepSize(), false);
      }
    } else if (e.key === "ArrowRight") {
      if (mode !== "webtoon") {
        e.preventDefault();
        showPage(isRTL() ? idx - stepSize() : idx + stepSize(), false);
      }
    } else if (e.key === " ") {
      if (mode !== "webtoon") {
        e.preventDefault();
        showPage(idx + stepSize(), false);
      }
    }
  });

  setBg(bg);
  setFit(fit);
  setMode(mode);
})();
</script>
`))

var sourceSettingsTmpl = template.Must(template.New("sourceSettings").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.DisplayName}} settings — Yatagarasu</title>` + baseHead + `
<style>
 .pref-head{display:flex;flex-wrap:wrap;gap:.75rem;align-items:center;justify-content:space-between;margin-bottom:1.15rem}
 .pref-list{display:flex;flex-direction:column;gap:.85rem}
 .pref-item{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:1rem 1.2rem}
 .pref-item h3{font-size:.96rem;margin-bottom:.2rem}
 .pref-sum{margin:0 0 .7rem;color:var(--muted);font-size:.825rem}
 .pref-row{display:flex;flex-wrap:wrap;gap:.55rem;align-items:center}
 .pref-row input[type=text],.pref-row select{padding:.44rem .68rem;font:inherit;font-size:.875rem;color:var(--ink);
   background:var(--bg);border:1px solid var(--line);border-radius:var(--radius);min-width:14rem;flex:1;max-width:26rem}
 .pref-checks{display:flex;flex-wrap:wrap;gap:.45rem .7rem;margin-bottom:.65rem}
 .pref-check{display:inline-flex;align-items:center;gap:.35rem;padding:.26rem .6rem;border-radius:999px;
             border:1px solid var(--line);background:var(--bg);font-size:.825rem;cursor:pointer}
 .pref-check input{accent-color:var(--accent-solid);margin:0}
</style>
<body><div class="wrap narrow">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<div class="pref-head">
  <div>
    <h1>{{.DisplayName}}</h1>
    <p class="hint" style="margin:.15rem 0 0;color:var(--muted);font-size:.84rem">Source preferences</p>
  </div>
  <a class="btn sm" href="/browse?tab=sources&source={{.SourceID}}">← Back to source</a>
</div>

{{if .Preferences}}
<div class="pref-list">
  {{range .Preferences}}
  <section class="pref-item">
    <h3>{{if .Title}}{{.Title}}{{else}}Preference #{{.Position}}{{end}}</h3>
    {{if .Summary}}<p class="pref-sum">{{.Summary}}</p>{{end}}
    <form method="post" action="/source/{{$.SourceID}}/settings">
      <input type="hidden" name="position" value="{{.Position}}">
      <input type="hidden" name="type" value="{{.Type}}">
      {{if or (eq .Type "CheckBoxPreference") (eq .Type "SwitchPreference")}}
      <div class="pref-row">
        <label class="pref-check">
          <input type="checkbox" name="value" value="true"{{if .CurrentBool}} checked{{end}}>
          <span>{{if .CurrentBool}}Enabled{{else}}Disabled{{end}}</span>
        </label>
        <button class="btn sm primary" type="submit">Save</button>
      </div>
      {{else if eq .Type "ListPreference"}}
      <div class="pref-row">
        <select name="value" aria-label="{{.Title}}">
          {{range .Options}}
          <option value="{{.Value}}"{{if .Selected}} selected{{end}}>{{.Label}}</option>
          {{end}}
        </select>
        <button class="btn sm primary" type="submit">Save</button>
      </div>
      {{else if eq .Type "MultiSelectListPreference"}}
      <div class="pref-checks">
        {{range .Options}}
        <label class="pref-check">
          <input type="checkbox" name="values" value="{{.Value}}"{{if .Selected}} checked{{end}}>
          <span>{{.Label}}</span>
        </label>
        {{end}}
      </div>
      <button class="btn sm primary" type="submit">Save</button>
      {{else if eq .Type "EditTextPreference"}}
      <div class="pref-row">
        <input type="text" name="value" value="{{.CurrentStr}}" aria-label="{{.Title}}">
        <button class="btn sm primary" type="submit">Save</button>
      </div>
      {{else}}
      <p class="hint">Unsupported preference type: <code>{{.Type}}</code></p>
      {{end}}
    </form>
  </section>
  {{end}}
</div>
{{else}}
<section class="empty">
  <h2>No configurable preferences</h2>
  <p>This source does not expose any settings.</p>
</section>
{{end}}

</div>
`))

var categoriesTmpl = template.Must(template.New("categories").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Categories — Yatagarasu</title>` + baseHead + `
<style>
 .cat-head{display:flex;flex-wrap:wrap;gap:.75rem;align-items:center;justify-content:space-between;margin-bottom:1.15rem}
 .cat-create{display:flex;flex-wrap:wrap;gap:.5rem;align-items:center}
 .cat-create input,.cat-rename input{padding:.42rem .68rem;font:inherit;font-size:.875rem;color:var(--ink);
   background:var(--bg);border:1px solid var(--line);border-radius:var(--radius);flex:1;min-width:11rem}
 .cat-rename{display:inline-flex;flex-wrap:wrap;gap:.4rem;align-items:center;width:100%;max-width:24rem}
</style>
<body><div class="wrap narrow">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<div class="cat-head">
  <h1>Categories</h1>
  <a class="btn sm" href="/library">← Back to library</a>
</div>

{{if not .Configured}}
<section class="empty">
  <h2>Suwayomi-Server is not connected</h2>
  <p>Configure your Suwayomi-Server URL in <a href="/settings#suwayomi">Settings</a> to manage library categories.</p>
</section>
{{else}}
<section class="panel">
  <div class="panel-head"><h2>New category</h2></div>
  <form class="cat-create" method="post" action="/categories">
    <input type="hidden" name="action" value="create">
    <input name="name" type="text" required placeholder="Category name (e.g. Reading, Seinen, Favorites)..." aria-label="Category name">
    <button class="btn sm primary" type="submit">Create</button>
  </form>
</section>

<section class="card-table">
  <header>
    <h2>Library categories</h2>
    <p>{{len .Categories}} {{if eq (len .Categories) 1}}category{{else}}categories{{end}}</p>
  </header>
  {{if .Categories}}
  <table>
    <thead><tr><th>Name</th><th class="size">Manga</th><th></th></tr></thead>
    <tbody>
      {{range .Categories}}
      <tr>
        <td>
          <form class="cat-rename" method="post" action="/categories">
            <input type="hidden" name="action" value="rename">
            <input type="hidden" name="id" value="{{.ID}}">
            <input name="name" type="text" value="{{.Name}}" required aria-label="Rename {{.Name}}">
            <button class="btn sm" type="submit">Rename</button>
          </form>
        </td>
        <td class="size"><span class="pill">{{.MangaCount}}</span></td>
        <td class="state">
          <form class="inline" method="post" action="/categories" data-confirm="Delete category {{.Name}}?">
            <input type="hidden" name="action" value="delete">
            <input type="hidden" name="id" value="{{.ID}}">
            <button class="btn ghost" type="submit">Delete</button>
          </form>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="hint" style="padding:1rem 1.25rem">No custom categories yet. Create one above to organize your library.</p>
  {{end}}
</section>
{{end}}

</div>
<script nonce="{{.Nonce}}">
for (const f of document.querySelectorAll("form[data-confirm]"))
  f.addEventListener("submit", e => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });
</script>
`))

var updatesTmpl = template.Must(template.New("updates").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Updates — Yatagarasu</title>` + baseHead + `
<style>
 .upd-head{display:flex;flex-wrap:wrap;gap:.75rem;align-items:center;justify-content:space-between;margin-bottom:1.15rem}
 .upd-manga{display:inline-flex;align-items:center;gap:.65rem;text-decoration:none;color:var(--ink)}
 .upd-manga:hover .upd-title{color:var(--accent)}
 .upd-cover{width:34px;height:48px;border-radius:.3rem;object-fit:cover;background:var(--raise);border:1px solid var(--line);flex-shrink:0}
 .upd-cover-ph{width:34px;height:48px;border-radius:.3rem;background:var(--raise);border:1px solid var(--line);flex-shrink:0}
 .upd-title{font-weight:580;display:block;line-height:1.25}
 .upd-ch{display:block;font-size:.82rem;color:var(--muted)}
 .upd-read .upd-title,.upd-read .upd-ch{opacity:.65}
</style>
<body><div class="wrap">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

{{if not .Configured}}
<section class="empty">
  <h2>Suwayomi-Server is not connected</h2>
  <p>Configure your Suwayomi-Server URL in <a href="/settings#suwayomi">Settings</a> to see recent chapter updates across your library.</p>
</section>
{{else}}
<div class="upd-head">
  <div>
    <h1>Recent Updates <span class="pill">{{.Total}}</span></h1>
  </div>
  <form method="post" action="/downloads/control" style="margin:0">
    <input type="hidden" name="action" value="update-library">
    <input type="hidden" name="redirect" value="/updates">
    <button class="btn sm primary" type="submit">Update Library</button>
  </form>
</div>

{{if .Groups}}
  {{range .Groups}}
  <section class="card-table">
    <header>
      <h2>{{.Label}}</h2>
      <p>{{len .Items}} {{if eq (len .Items) 1}}chapter{{else}}chapters{{end}}</p>
    </header>
    <table>
      <tbody>
        {{range .Items}}
        <tr{{if .IsRead}} class="upd-read"{{end}}>
          <td>
            <a class="upd-manga" href="/manga/{{.MangaID}}">
              {{if .ThumbnailURL}}<img class="upd-cover" src="{{.ThumbnailURL}}" alt="" loading="lazy">{{else}}<span class="upd-cover-ph"></span>{{end}}
              <span>
                <span class="upd-title">{{.MangaTitle}}</span>
                <span class="upd-ch">{{.Name}}{{if .Scanlator}} · {{.Scanlator}}{{end}}</span>
              </span>
            </a>
          </td>
          <td class="when">{{.When}}</td>
          <td class="state">
            <a class="btn sm primary" href="/read/{{.ID}}">Read</a>
            {{if .IsDownloaded}}
            <span class="tag">saved</span>
            {{else}}
            <form class="inline" method="post" action="/manga/{{.MangaID}}/chapter/{{.ID}}">
              <input type="hidden" name="action" value="download">
              <input type="hidden" name="redirect" value="/updates">
              <button class="btn sm" type="submit">Download</button>
            </form>
            {{end}}
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </section>
  {{end}}
{{else}}
<section class="empty">
  <h2>No recent updates yet</h2>
  <p>Click <strong>Update Library</strong> above to check installed sources for new chapters.</p>
</section>
{{end}}
{{end}}

</div>
`))

var migrateTmpl = template.Must(template.New("migrate").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Migrate {{.Manga.Title}} — Yatagarasu</title>` + baseHead + `
<style>
 .mig-banner{display:flex;flex-wrap:wrap;gap:1rem;align-items:center;justify-content:space-between;
             background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:1rem 1.2rem;margin-bottom:1.25rem}
 .mig-src{display:flex;align-items:center;gap:.85rem}
 .mig-thumb{width:46px;height:66px;border-radius:.35rem;object-fit:cover;background:var(--raise);border:1px solid var(--line)}
 .mig-search{display:flex;gap:.45rem;align-items:center;flex:1;min-width:14rem;max-width:24rem}
 .mig-search input{flex:1;padding:.42rem .68rem;font:inherit;font-size:.875rem;color:var(--ink);
   background:var(--bg);border:1px solid var(--line);border-radius:var(--radius)}
 .mig-group{margin-bottom:1.5rem}
 .mig-head{display:flex;align-items:center;justify-content:space-between;gap:.75rem;margin-bottom:.75rem;
           padding-bottom:.45rem;border-bottom:1px solid var(--line)}
 .mig-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(10rem,1fr));gap:1rem .85rem}
 .mig-card{display:flex;flex-direction:column;background:var(--surface);border:1px solid var(--line);
           border-radius:var(--radius);padding:.6rem;gap:.45rem}
 .mig-cover{aspect-ratio:2/3;border-radius:.35rem;overflow:hidden;background:var(--raise);border:1px solid var(--line)}
 .mig-cover img{width:100%;height:100%;object-fit:cover;display:block}
 .mig-title{font-size:.84rem;font-weight:560;line-height:1.3;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
</style>
<body><div class="wrap">
` + navHeader + `

{{if .Problem}}<p class="flash bad">{{.Problem}}</p>{{end}}
{{if .Note}}<p class="flash ok">{{.Note}}</p>{{end}}

<section class="mig-banner">
  <div class="mig-src">
    {{if .Manga.ThumbnailURL}}<img class="mig-thumb" src="{{.Manga.ThumbnailURL}}" alt="">{{end}}
    <div>
      <h1>Migrate “{{.Manga.Title}}”</h1>
      <p class="hint" style="margin:.15rem 0 0;color:var(--muted);font-size:.825rem">
        From {{if .Manga.SourceName}}{{.Manga.SourceName}}{{else}}current source{{end}} · Copies read chapters &amp; categories to the new source
      </p>
    </div>
  </div>
  <div style="display:flex;flex-wrap:wrap;gap:.5rem;align-items:center">
    <form class="mig-search" method="get" action="/manga/{{.Manga.ID}}/migrate">
      <input name="q" value="{{.Query}}" placeholder="Search title across sources..." aria-label="Search migration title">
      <button class="btn sm" type="submit">Search</button>
    </form>
    <a class="btn sm ghost" href="/manga/{{.Manga.ID}}">← Cancel</a>
  </div>
</section>

{{if .Groups}}
  {{range .Groups}}
  <section class="mig-group">
    <div class="mig-head">
      <h2>{{.Source.Name}} <span class="tag muted">{{.Source.Lang}}</span></h2>
    </div>
    <div class="mig-grid">
      {{range .Mangas}}
      <div class="mig-card">
        <a class="mig-cover" href="/manga/{{.ID}}">
          {{if .ThumbnailURL}}<img src="{{.ThumbnailURL}}" alt="{{.Title}}" loading="lazy">{{end}}
        </a>
        <span class="mig-title">{{.Title}}</span>
        <form method="post" action="/manga/{{$.Manga.ID}}/migrate" data-confirm="Migrate {{$.Manga.Title}} to {{.Title}}? This will copy read progress and remove the old entry from your library.">
          <input type="hidden" name="targetMangaId" value="{{.ID}}">
          <button class="btn sm primary" style="width:100%;justify-content:center" type="submit">Migrate here</button>
        </form>
      </div>
      {{end}}
    </div>
  </section>
  {{end}}
{{else}}
<section class="empty">
  <h2>No migration candidates found for “{{.Query}}”</h2>
  <p>Try searching with an alternate title above or install additional extensions.</p>
</section>
{{end}}

</div>
<script nonce="{{.Nonce}}">
for (const f of document.querySelectorAll("form[data-confirm]"))
  f.addEventListener("submit", e => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });
</script>
`))

