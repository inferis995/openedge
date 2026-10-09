import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowLeft, ChevronLeft, ChevronRight, Tv } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import PinnedSynopticCard from '@/components/synoptics/PinnedSynopticCard';
import { usePinnedSynoptics } from '@/hooks/usePinnedSynoptics';

// TV mode for pinned synoptics: full-screen dark page rotating through the
// pinned pages every 15 s. Same pattern as the OEE kiosk (/tv/oee).
const ROTATE_MS = 15_000;

export default function TvSynopticsPage() {
    const { t } = useTranslation();
    const { pins } = usePinnedSynoptics();
    const [idx, setIdx] = useState(0);

    useEffect(() => {
        if (pins.length <= 1) return;
        const id = setInterval(() => setIdx(i => (i + 1) % pins.length), ROTATE_MS);
        return () => clearInterval(id);
    }, [pins.length]);

    const safeIdx = pins.length === 0 ? 0 : idx % pins.length;

    if (pins.length === 0) {
        return (
            <div className="min-h-screen flex flex-col items-center justify-center gap-4 bg-black text-white">
                <Tv size={40} className="text-slate-500" />
                <p className="text-lg text-slate-300">{t('tvSynoptics.empty')}</p>
                <Link to="/synoptics" className="text-sm underline text-slate-400">{t('tvSynoptics.go_pin')}</Link>
            </div>
        );
    }

    const current = pins[safeIdx];

    return (
        <div className="min-h-screen bg-black text-white flex flex-col">
            <div className="flex items-center justify-between px-4 py-2 text-slate-400">
                <Link to="/" className="flex items-center gap-1 text-xs hover:text-white">
                    <ArrowLeft size={14} /> {t('tvSynoptics.exit')}
                </Link>
                <span className="text-xs tabular-nums">{safeIdx + 1} / {pins.length}</span>
            </div>
            <div className="flex-1 flex items-center justify-center px-4 pb-4 min-h-0">
                <PinnedSynopticCard
                    key={current}
                    synopticId={current}
                    height={Math.max(300, window.innerHeight - 120)}
                    maxWidth={Math.max(280, window.innerWidth - 32)}
                    showTitle={false}
                />
            </div>
            {pins.length > 1 && (
                <div className="flex items-center justify-center gap-4 pb-4">
                    <button
                        type="button"
                        className="p-2 rounded-full border border-slate-700 hover:bg-slate-800"
                        onClick={() => setIdx((safeIdx - 1 + pins.length) % pins.length)}
                        aria-label="prev"
                    >
                        <ChevronLeft size={18} />
                    </button>
                    <div className="flex gap-1.5">
                        {pins.map((p, i) => (
                            <button
                                key={p}
                                type="button"
                                aria-label={`page ${i + 1}`}
                                onClick={() => setIdx(i)}
                                className={`w-2 h-2 rounded-full ${i === idx ? 'bg-white' : 'bg-slate-600'}`}
                            />
                        ))}
                    </div>
                    <button
                        type="button"
                        className="p-2 rounded-full border border-slate-700 hover:bg-slate-800"
                        onClick={() => setIdx((safeIdx + 1) % pins.length)}
                        aria-label="next"
                    >
                        <ChevronRight size={18} />
                    </button>
                </div>
            )}
        </div>
    );
}
