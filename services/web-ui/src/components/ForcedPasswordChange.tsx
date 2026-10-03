import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { KeyRound, LogOut } from 'lucide-react';
import { profileApi } from '@/api/profile';
import { useAuthStore } from '@/stores/useAuthStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

/**
 * What an account still on the default password sees instead of any page.
 *
 * The server refuses every other request from such a session
 * (middleware.RequireAuth), so showing the application would only show
 * errors. On success the server hands back a token without the restriction
 * and the user carries on where they were.
 */
export default function ForcedPasswordChange() {
    const { t } = useTranslation();
    const { user, token, login, logout } = useAuthStore();
    const [oldPw, setOldPw] = useState('');
    const [newPw, setNewPw] = useState('');
    const [confirmPw, setConfirmPw] = useState('');
    const [error, setError] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);

    const submit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError(null);
        if (newPw !== confirmPw) { setError(t('profilePage.pw_mismatch')); return; }
        if (newPw.length < 12) { setError(t('profilePage.pw_too_short')); return; }
        setBusy(true);
        try {
            const res = await profileApi.changePassword(oldPw, newPw);
            if (res.token && user) {
                login(res.token, { ...user, must_change_password: false });
            } else {
                // No new token: the old one is still confined. Sign in again.
                logout();
                window.alert(t('forcedPassword.relogin'));
            }
        } catch (err) {
            const ax = err as { response?: { data?: { error?: string } } };
            setError(ax.response?.data?.error ?? t('profilePage.pw_failed'));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="min-h-screen flex items-center justify-center bg-background p-4">
            <form onSubmit={submit} className="w-full max-w-md rounded-xl border bg-card p-6 space-y-5">
                <div className="flex items-start gap-3">
                    <div className="p-2 rounded-lg bg-amber-500/10 text-amber-600 shrink-0">
                        <KeyRound className="h-5 w-5" />
                    </div>
                    <div>
                        <h1 className="text-lg font-semibold">{t('forcedPassword.title')}</h1>
                        <p className="text-sm text-muted-foreground mt-1">{t('forcedPassword.intro')}</p>
                        {user && <p className="text-xs text-muted-foreground mt-2 font-mono">{user.username}</p>}
                    </div>
                </div>

                <div className="space-y-1.5">
                    <Label htmlFor="fpc-old">{t('profilePage.current_password')}</Label>
                    <Input id="fpc-old" type="password" autoComplete="current-password" required
                        value={oldPw} onChange={(e) => setOldPw(e.target.value)} />
                </div>
                <div className="space-y-1.5">
                    <Label htmlFor="fpc-new">{t('profilePage.new_password')}</Label>
                    <Input id="fpc-new" type="password" autoComplete="new-password" required minLength={12}
                        placeholder={t('profilePage.new_password_placeholder')}
                        value={newPw} onChange={(e) => setNewPw(e.target.value)} />
                </div>
                <div className="space-y-1.5">
                    <Label htmlFor="fpc-confirm">{t('profilePage.confirm_password')}</Label>
                    <Input id="fpc-confirm" type="password" autoComplete="new-password" required
                        placeholder={t('profilePage.confirm_placeholder')}
                        value={confirmPw} onChange={(e) => setConfirmPw(e.target.value)} />
                </div>

                {error && <p role="alert" className="text-sm text-destructive">{error}</p>}

                <div className="flex items-center justify-between gap-3">
                    <Button type="button" variant="ghost" onClick={logout} disabled={busy}>
                        <LogOut className="h-4 w-4 mr-2" /> {t('forcedPassword.logout')}
                    </Button>
                    <Button type="submit" disabled={busy || !token}>
                        {busy ? t('profilePage.updating') : t('forcedPassword.submit')}
                    </Button>
                </div>
            </form>
        </div>
    );
}
