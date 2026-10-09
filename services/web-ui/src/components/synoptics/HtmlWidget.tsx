import { useEffect, useMemo, useRef } from 'react';
import type { LiveValue } from './SynopticWidget';

export interface HtmlBinding {
    prop: string;
    tagId: number | null;
}

// The bridge injected into every imported page. It speaks the AVEVA custom-
// widget dialect (`cwidget.on(prop, cb)` + `cwidget[prop]` reads) so pages
// authored for AVEVA run unchanged: the AVEVA <script src> tags are stripped
// on import and this shim takes their place, fed by postMessage from React.
const BRIDGE_JS = `
(function () {
  var listeners = {};
  var store = {};
  function fire(name) {
    var cbs = listeners[name] || [];
    for (var i = 0; i < cbs.length; i++) { try { cbs[i](name); } catch (e) {} }
  }
  var cw = {
    on: function (name, cb) { (listeners[name] = listeners[name] || []).push(cb); },
    off: function (name, cb) {
      listeners[name] = (listeners[name] || []).filter(function (f) { return f !== cb; });
    },
  };
  window.cwidget = new Proxy(cw, {
    get: function (t, k) {
      if (k in t) return t[k];
      return store[k];
    },
    set: function (t, k, v) {
      // Writes from the page stay local in v1 (monitors are read-only);
      // the day a page needs writes this is the single hook to wire through.
      store[k] = v;
      fire(k);
      return true;
    },
  });
  window.addEventListener('message', function (e) {
    var d = e.data;
    if (!d || d.type !== 'oe-values') return;
    var vals = d.values || {};
    for (var k in vals) {
      if (store[k] !== vals[k]) { store[k] = vals[k]; fire(k); }
    }
  });
  window.parent.postMessage({ type: 'oe-ready' }, '*');
})();
`;

// AVEVA runtime scripts (proxy.js / MASvrHelper.js): provided by the bridge.
const AVEVA_SCRIPT_RE = /<script[^>]+src=["'][^"']*resources\/apis\/[^"']*["'][^>]*>\s*<\/script>\s*/gi;

export function sanitizeImportedHtml(raw: string): string {
    return raw.replace(AVEVA_SCRIPT_RE, '');
}

// Custom-property names the page expects. Declared first via
// <meta name="oe-props" content="pressione1, batteria, ts"> (or an
// <!-- oe-props: ... --> comment), then anything the code reads through
// cwidget.on('x') / cwidget['x']. The editor offers them for tag binding.
export function extractProps(source: string): string[] {
    const out = new Set<string>();
    const meta = source.match(/<meta[^>]+name=["']oe-props["'][^>]*content=["']([^"']*)["']/i);
    const comment = !meta && source.match(/<!--\s*oe-props\s*:\s*([^-]*)-->/i);
    const declared = (meta && meta[1]) || (comment && comment[1]) || '';
    if (declared) {
        for (const name of declared.split(',')) {
            const n = name.trim();
            if (/^[A-Za-z_]\w*$/.test(n)) out.add(n);
        }
    }
    const re = /cwidget\s*(?:\.on\s*\(\s*|\[\s*)['"]([A-Za-z_]\w*)['"]/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(source)) !== null) out.add(m[1]);
    return [...out];
}

export function buildSrcDoc(source: string): string {
    const bridge = `<script>${BRIDGE_JS}</script>`;
    if (/<\/body\s*>/i.test(source)) return source.replace(/<\/body\s*>/i, `${bridge}</body>`);
    return `${source}\n${bridge}`;
}

export default function HtmlWidget({
    htmlSource,
    bindings,
    tsProp,
    liveMap,
    title,
}: {
    htmlSource?: string;
    bindings?: HtmlBinding[];
    tsProp?: string;
    liveMap?: Map<number, LiveValue>;
    title?: string;
}) {
    const frameRef = useRef<HTMLIFrameElement>(null);

    const srcDoc = useMemo(
        () => (htmlSource ? buildSrcDoc(htmlSource) : null),
        [htmlSource],
    );

    // Newest sample time across the bound tags (unix seconds, like AVEVA ts).
    const values = useMemo(() => {
        const vals: Record<string, unknown> = {};
        let newest = 0;
        for (const b of bindings ?? []) {
            if (b.tagId == null) continue;
            const lv = liveMap?.get(b.tagId);
            if (lv?.value !== undefined && lv?.value !== null) vals[b.prop] = lv.value;
            if (lv?.timestamp && lv.timestamp > newest) newest = lv.timestamp;
        }
        if (tsProp && newest > 0) vals[tsProp] = Math.floor(newest / 1000);
        return vals;
    }, [bindings, tsProp, liveMap]);

    useEffect(() => {
        frameRef.current?.contentWindow?.postMessage({ type: 'oe-values', values }, '*');
    }, [values, srcDoc]);

    // The frame drops messages posted before it finishes loading (and the
    // bridge announces itself with oe-ready): resend on both, so values
    // already held are never lost on first open.
    useEffect(() => {
        const onMsg = (e: MessageEvent) => {
            if (e.data?.type !== 'oe-ready') return;
            frameRef.current?.contentWindow?.postMessage({ type: 'oe-values', valuesRef.current }, '*');
        };
        window.addEventListener('message', onMsg);
        return () => window.removeEventListener('message', onMsg);
    }, []);
    const valuesRef = useRef(values);
    valuesRef.current = values;

    if (!srcDoc) {
        return (
            <div className="w-full h-full flex items-center justify-center rounded-md border border-dashed border-slate-600 text-[11px] text-slate-400 px-3 text-center">
                {title ?? 'HTML'}
            </div>
        );
    }
    return (
        <iframe
            ref={frameRef}
            title={title ?? 'HTML widget'}
            className="w-full h-full rounded-md border-0 bg-white"
            sandbox="allow-scripts"
            srcDoc={srcDoc}
            onLoad={() => {
                frameRef.current?.contentWindow?.postMessage({ type: 'oe-values', valuesRef.current }, '*');
            }}
        />
    );
}
