import { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { invitesApi } from '@/api/invites';
import { CheckCircle, Loader2, ShieldCheck } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export default function AcceptInvitePage() {
    const navigate = useNavigate();
    const { t } = useTranslation();
    const [searchParams] = useSearchParams();
    const token = searchParams.get('token') ?? '';

    const [form, setForm] = useState({ username: '', password: '', confirmPassword: '', full_name: '' });
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [done, setDone] = useState(false);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError('');

        if (!token) {
            setError(t('acceptInvite.missing_token'));
            return;
        }
        if (form.password !== form.confirmPassword) {
            setError(t('acceptInvite.mismatch'));
            return;
        }
        if (form.password.length < 12) {
            setError(t('acceptInvite.too_short'));
            return;
        }

        setLoading(true);
        try {
            await invitesApi.accept({
                token,
                username: form.username,
                password: form.password,
                full_name: form.full_name || undefined,
            });
            setDone(true);
        } catch (err: unknown) {
            const axiosErr = err as { response?: { data?: { error?: string } } };
            const msg = axiosErr?.response?.data?.error || t('acceptInvite.failed');
            setError(msg);
        } finally {
            setLoading(false);
        }
    };

    if (!token) {
        return (
            <div className="min-h-screen flex items-center justify-center bg-background p-4">
                <div className="max-w-md w-full text-center space-y-4">
                    <p className="text-destructive font-medium">{t('acceptInvite.invalid_link')}</p>
                    <Button variant="outline" onClick={() => navigate('/login')}>{t('acceptInvite.go_login')}</Button>
                </div>
            </div>
        );
    }

    if (done) {
        return (
            <div className="min-h-screen flex items-center justify-center bg-background p-4">
                <div className="max-w-md w-full text-center space-y-4">
                    <CheckCircle size={48} className="mx-auto text-green-500" />
                    <h1 className="text-2xl font-bold">{t('acceptInvite.done_title')}</h1>
                    <p className="text-muted-foreground">
                        {t('acceptInvite.done_before')}<strong>{form.username}</strong>{t('acceptInvite.done_after')}
                    </p>
                    <Button onClick={() => navigate('/login')}>{t('acceptInvite.go_login')}</Button>
                </div>
            </div>
        );
    }

    return (
        <div className="min-h-screen flex items-center justify-center bg-background p-4">
            <div className="max-w-md w-full space-y-6">
                {/* Header */}
                <div className="text-center space-y-2">
                    <div className="flex justify-center">
                        <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10">
                            <ShieldCheck size={28} className="text-primary" />
                        </div>
                    </div>
                    <h1 className="text-2xl font-bold tracking-tight">{t('acceptInvite.title')}</h1>
                    <p className="text-muted-foreground text-sm">
                        {t('acceptInvite.subtitle')}
                    </p>
                </div>

                {/* Form */}
                <div className="rounded-xl border bg-card p-6 shadow-xs space-y-5">
                    <form onSubmit={handleSubmit} className="space-y-4">
                        <div className="space-y-1.5">
                            <Label htmlFor="full_name">{t('acceptInvite.full_name')} <span className="text-muted-foreground">{t('acceptInvite.optional')}</span></Label>
                            <Input
                                id="full_name"
                                placeholder={t('acceptInvite.full_name_placeholder')}
                                value={form.full_name}
                                onChange={(e) => setForm((f) => ({ ...f, full_name: e.target.value }))}
                                autoComplete="name"
                            />
                        </div>

                        <div className="space-y-1.5">
                            <Label htmlFor="username">{t('acceptInvite.username')}</Label>
                            <Input
                                id="username"
                                placeholder={t('acceptInvite.username_placeholder')}
                                value={form.username}
                                onChange={(e) => setForm((f) => ({ ...f, username: e.target.value }))}
                                autoComplete="username"
                                required
                                minLength={3}
                            />
                        </div>

                        <div className="space-y-1.5">
                            <Label htmlFor="password">{t('acceptInvite.password')}</Label>
                            <Input
                                id="password"
                                type="password"
                                placeholder={t('acceptInvite.password_placeholder')}
                                value={form.password}
                                onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
                                autoComplete="new-password"
                                required
                                minLength={12}
                            />
                        </div>

                        <div className="space-y-1.5">
                            <Label htmlFor="confirm">{t('acceptInvite.confirm_password')}</Label>
                            <Input
                                id="confirm"
                                type="password"
                                placeholder={t('acceptInvite.confirm_placeholder')}
                                value={form.confirmPassword}
                                onChange={(e) => setForm((f) => ({ ...f, confirmPassword: e.target.value }))}
                                autoComplete="new-password"
                                required
                            />
                        </div>

                        {error && (
                            <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2">
                                <p className="text-sm text-destructive">{error}</p>
                            </div>
                        )}

                        <Button type="submit" className="w-full" disabled={loading}>
                            {loading && <Loader2 size={15} className="mr-2 animate-spin" />}
                            {loading ? t('acceptInvite.creating') : t('acceptInvite.submit')}
                        </Button>
                    </form>

                    <p className="text-center text-xs text-muted-foreground">
                        {t('acceptInvite.have_account')}{' '}
                        <button
                            type="button"
                            className="underline hover:text-foreground transition-colors"
                            onClick={() => navigate('/login')}
                        >
                            {t('acceptInvite.sign_in')}
                        </button>
                    </p>
                </div>
            </div>
        </div>
    );
}
