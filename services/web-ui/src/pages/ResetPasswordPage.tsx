import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { KeyRound, ArrowLeft, CheckCircle2, Eye, EyeOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { passwordResetApi } from '@/api/passwordReset';
import { useTranslation } from 'react-i18next';

export default function ResetPasswordPage() {
    const { t } = useTranslation();
    const [params] = useSearchParams();
    const token = params.get('token') ?? '';

    const [password, setPassword] = useState('');
    const [confirm, setConfirm] = useState('');
    const [showPw, setShowPw] = useState(false);
    const [loading, setLoading] = useState(false);
    const [done, setDone] = useState(false);
    const [error, setError] = useState('');

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (password !== confirm) {
            setError(t('resetPassword.mismatch'));
            return;
        }
        if (password.length < 12) {
            setError(t('resetPassword.too_short'));
            return;
        }
        setLoading(true);
        setError('');
        try {
            await passwordResetApi.reset(token, password);
            setDone(true);
        } catch (err: unknown) {
            const axiosErr = err as { response?: { data?: { error?: string } } };
            setError(axiosErr.response?.data?.error ?? t('resetPassword.failed'));
        } finally {
            setLoading(false);
        }
    };

    if (!token) {
        return (
            <div className="min-h-screen flex items-center justify-center bg-background px-4">
                <div className="text-center space-y-3">
                    <p className="text-destructive font-medium">{t('resetPassword.invalid_link')}</p>
                    <Link to="/forgot-password">
                        <Button variant="outline">{t('resetPassword.request_new')}</Button>
                    </Link>
                </div>
            </div>
        );
    }

    return (
        <div className="min-h-screen flex items-center justify-center bg-background px-4">
            <div className="w-full max-w-sm space-y-6">
                <div className="flex flex-col items-center gap-2 text-center">
                    <div className="h-12 w-12 rounded-xl bg-primary/10 flex items-center justify-center">
                        <KeyRound className="h-9 sm:h-6 w-9 sm:w-6 text-primary" />
                    </div>
                    <h1 className="text-2xl font-bold">{t('resetPassword.title')}</h1>
                    <p className="text-sm text-muted-foreground">
                        {t('resetPassword.subtitle')}
                    </p>
                </div>

                {done ? (
                    <div className="rounded-xl border bg-card p-6 text-center space-y-3">
                        <CheckCircle2 className="h-10 w-10 text-green-500 mx-auto" />
                        <p className="font-medium">{t('resetPassword.done_title')}</p>
                        <p className="text-sm text-muted-foreground">{t('resetPassword.done_desc')}</p>
                        <Link to="/login">
                            <Button className="w-full mt-2">{t('resetPassword.go_login')}</Button>
                        </Link>
                    </div>
                ) : (
                    <form onSubmit={handleSubmit} className="rounded-xl border bg-card p-6 space-y-4">
                        <div className="space-y-1.5">
                            <Label htmlFor="password">{t('resetPassword.new_password')}</Label>
                            <div className="relative">
                                <Input
                                    id="password"
                                    type={showPw ? 'text' : 'password'}
                                    placeholder={t('resetPassword.password_placeholder')}
                                    value={password}
                                    onChange={e => setPassword(e.target.value)}
                                    required
                                    autoFocus
                                />
                                <button
                                    type="button"
                                    onClick={() => setShowPw(v => !v)}
                                    className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                                    tabIndex={-1}
                                >
                                    {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                                </button>
                            </div>
                        </div>
                        <div className="space-y-1.5">
                            <Label htmlFor="confirm">{t('resetPassword.confirm_password')}</Label>
                            <Input
                                id="confirm"
                                type="password"
                                placeholder={t('resetPassword.confirm_placeholder')}
                                value={confirm}
                                onChange={e => setConfirm(e.target.value)}
                                required
                            />
                        </div>
                        {error && <p className="text-sm text-destructive">{error}</p>}
                        <Button type="submit" className="w-full" disabled={loading || !password || !confirm}>
                            {loading ? t('resetPassword.updating') : t('resetPassword.submit')}
                        </Button>
                    </form>
                )}

                <Link to="/login" className="flex items-center justify-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors">
                    <ArrowLeft className="h-4 w-4" /> {t('resetPassword.back_to_login')}
                </Link>
            </div>
        </div>
    );
}
