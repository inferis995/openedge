import { useState } from 'react';
import Sidebar from './Sidebar';
import Header from './Header';
import UpdateNotificationBanner from './UpdateNotificationBanner';
import { Toaster } from 'sonner';

interface MainLayoutProps {
    children: React.ReactNode;
}

/**
 * The application shell.
 *
 * The drawer state lives here because this is the only place that renders both
 * the button that opens it and the panel that opens: a store for it would make
 * a piece of ephemeral layout state reachable — and settable — from anywhere.
 */
const MainLayout = ({ children }: MainLayoutProps) => {
    const [navOpen, setNavOpen] = useState(false);

    return (
        <div className="flex h-screen bg-background">
            <Sidebar mobileOpen={navOpen} onMobileClose={() => setNavOpen(false)} />
            {/* min-w-0 is what stops a wide table from pushing the whole shell
                sideways: without it a flex child refuses to shrink below its
                content, and the page scrolls horizontally instead of the table. */}
            <div className="flex-1 flex flex-col overflow-hidden min-w-0">
                <Header onMenuClick={() => setNavOpen(true)} />
                <UpdateNotificationBanner />
                <main className="flex-1 overflow-auto p-4 md:p-6">
                    <div className="mx-auto max-w-7xl animate-in fade-in duration-500">
                        {children}
                    </div>
                </main>
            </div>
            <Toaster position="top-right" richColors />
        </div>
    );
};

export default MainLayout;
