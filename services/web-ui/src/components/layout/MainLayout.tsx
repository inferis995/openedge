import Sidebar from './Sidebar';
import Header from './Header';
import UpdateNotificationBanner from './UpdateNotificationBanner';
import { Toaster } from 'sonner';

interface MainLayoutProps {
    children: React.ReactNode;
}

const MainLayout = ({ children }: MainLayoutProps) => {
    return (
        <div className="flex h-screen bg-background">
            <Sidebar />
            <div className="flex-1 flex flex-col overflow-hidden">
                <Header />
                <UpdateNotificationBanner />
                <main className="flex-1 overflow-auto p-6">
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
