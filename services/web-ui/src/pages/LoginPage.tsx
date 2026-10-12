import { useState, useEffect, useRef } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuthStore } from '@/stores/useAuthStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Lock, User as UserIcon, Loader2, Mail, Eye, EyeOff, ShieldCheck } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RecoveryCodes } from '@/components/auth/RecoveryCodes';

const LoginPage = () => {
    const { t } = useTranslation();
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);
    const [showPassword, setShowPassword] = useState(false);
    const [ssoEmail, setSsoEmail] = useState('');
    const [showSsoInput, setShowSsoInput] = useState<'google' | 'azure' | null>(null);
    const [mfaStep, setMfaStep] = useState(false);
    const [mfaToken, setMfaToken] = useState('');
    const [mfaCode, setMfaCode] = useState('');
    // A recovery code (XXXX-XXXX-XXXX) instead of the authenticator's six digits.
    const [useRecovery, setUseRecovery] = useState(false);
    // Enrolment: the organization requires MFA and this user has none yet.
    const [setupToken, setSetupToken] = useState('');
    const [enroll, setEnroll] = useState<{ secret: string; qr_png?: string } | null>(null);
    const [pendingLogin, setPendingLogin] = useState<{ token: string; user: Parameters<ReturnType<typeof useAuthStore.getState>['login']>[1]; codes: string[] } | null>(null);
    const mfaInputRef = useRef<HTMLInputElement>(null);
    const navigate = useNavigate();
    const { login, isAuthenticated } = useAuthStore();

    // The SSO callback token is consumed at app boot by consumeSSOToken()
    // (src/lib/ssoBootstrap.ts) — it has to happen before the router mounts,
    // because RequireAuth's redirect to /login discards the URL fragment the
    // token arrives in. Here we only need to leave the login page once that
    // bootstrap has established a session.
    useEffect(() => {
        if (isAuthenticated()) {
            navigate('/', { replace: true });
        }
    }, [isAuthenticated, navigate]);

    const handleSSOLogin = (provider: 'google' | 'azure') => {
        if (!ssoEmail.trim()) {
            setShowSsoInput(provider);
            return;
        }
        window.location.href = `/api/auth/sso/${provider}/login?email=${encodeURIComponent(ssoEmail.trim())}`;
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError('');
        setLoading(true);

        try {
            const response = await fetch('/api/auth/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password }),
            });

            if (!response.ok) {
                const body = await response.json().catch(() => ({}));
                throw new Error(body.error || t('login.invalid_credentials'));
            }

            const data = await response.json();

            if (data.mfa_required) {
                setMfaToken(data.mfa_token);
                setMfaStep(true);
                setTimeout(() => mfaInputRef.current?.focus(), 100);
                return;
            }
            if (data.mfa_setup_required && data.mfa_setup_token) {
                // The organization requires MFA: set it up now, then sign in.
                const start = await fetch('/api/auth/mfa/enroll/start', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ mfa_setup_token: data.mfa_setup_token }),
                });
                const body = await start.json().catch(() => ({}));
                if (!start.ok) throw new Error(body.error || t('mfa.enroll_failed'));
                setSetupToken(data.mfa_setup_token);
                setEnroll({ secret: body.secret, qr_png: body.qr_png });
                setMfaCode('');
                setTimeout(() => mfaInputRef.current?.focus(), 100);
                return;
            }
            if (!data.token) throw new Error(t('login.invalid_credentials'));

            login(data.token, data.user);
            navigate('/');
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : t('login.invalid_credentials'));
        } finally {
            setLoading(false);
        }
    };

    const handleMFASubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError('');
        setLoading(true);
        try {
            const response = enroll
                ? await fetch('/api/auth/mfa/enroll/finish', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ mfa_setup_token: setupToken, code: mfaCode }),
                })
                : await fetch('/api/auth/mfa/verify', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ mfa_token: mfaToken, code: mfaCode.trim().toUpperCase() }),
                });
            if (!response.ok) {
                const body = await response.json().catch(() => ({}));
                throw new Error(body.error || t('login.mfa_invalid'));
            }
            const data = await response.json();
            if (enroll && Array.isArray(data.recovery_codes) && data.recovery_codes.length) {
                // Shown once, before entering: they are the way back in without the phone.
                setPendingLogin({ token: data.token, user: data.user, codes: data.recovery_codes });
                return;
            }
            login(data.token, data.user);
            navigate('/');
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : t('login.mfa_invalid'));
            setMfaCode('');
            mfaInputRef.current?.focus();
        } finally {
            setLoading(false);
        }
    };

    const resetMfa = () => {
        setMfaStep(false); setEnroll(null); setSetupToken(''); setPendingLogin(null);
        setUseRecovery(false); setMfaCode(''); setError('');
    };
    const codeOk = useRecovery ? /^[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}$/i.test(mfaCode.trim()) : mfaCode.length === 6;

    if (mfaStep || enroll || pendingLogin) {
        return (
            <div className="flex min-h-screen items-center justify-center bg-background p-4">
                <div className="w-full max-w-sm space-y-6 rounded-2xl border bg-card p-6 sm:p-8 shadow-xl">
                    <div className="text-center">
                        <div className="flex justify-center mb-4">
                            <div className="h-14 w-14 rounded-2xl bg-muted flex items-center justify-center">
                                <ShieldCheck className="h-7 w-7" />
                            </div>
                        </div>
                        <h2 className="text-lg font-semibold">{pendingLogin ? t('mfa.enrolled_title') : enroll ? t('mfa.enroll_title') : t('login.mfa_title')}</h2>
                        <p className="mt-1 text-sm text-muted-foreground">
                            {pendingLogin ? t('mfa.enrolled_desc') : enroll ? t('mfa.enroll_desc') : useRecovery ? t('mfa.recovery_login_desc') : t('login.mfa_desc')}
                        </p>
                    </div>

                    {pendingLogin ? (
                        <div className="space-y-5">
                            <RecoveryCodes codes={pendingLogin.codes} />
                            <Button className="w-full" onClick={() => { login(pendingLogin.token, pendingLogin.user); navigate('/'); }}>
                                {t('mfa.continue')}
                            </Button>
                        </div>
                    ) : (
                    <form className="space-y-5" onSubmit={handleMFASubmit}>
                        {enroll && (
                            <div className="space-y-3">
                                {enroll.qr_png && (
                                    <div className="flex justify-center">
                                        <img src={enroll.qr_png} alt={t('profilePage.mfa_qr_alt')} className="h-44 w-44 rounded-lg border bg-white p-2" />
                                    </div>
                                )}
                                <div className="grid gap-1">
                                    <Label className="text-xs text-muted-foreground">{t('profilePage.mfa_manual_key')}</Label>
                                    <code className="select-all break-all rounded-md bg-muted px-3 py-2 text-xs">{enroll.secret}</code>
                                </div>
                            </div>
                        )}
                        <div className="grid gap-2">
                            <Label className="text-xs text-muted-foreground font-medium">{useRecovery ? t('mfa.recovery_code') : t('login.mfa_code')}</Label>
                            <Input
                                ref={mfaInputRef}
                                type="text"
                                inputMode={useRecovery ? 'text' : 'numeric'}
                                maxLength={useRecovery ? 14 : 6}
                                placeholder={useRecovery ? 'XXXX-XXXX-XXXX' : '000000'}
                                value={mfaCode}
                                onChange={e => setMfaCode(useRecovery
                                    ? e.target.value.toUpperCase().replace(/[^0-9A-F-]/g, '')
                                    : e.target.value.replace(/\D/g, ''))}
                                className="h-12 text-center text-xl tracking-[0.3em] font-mono"
                                autoComplete="one-time-code"
                            />
                        </div>
                        {error && (
                            <p className="text-sm font-medium text-destructive text-center">{error}</p>
                        )}
                        <Button type="submit" className="w-full" disabled={loading || !codeOk}>
                            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : enroll ? t('mfa.enroll_confirm') : t('login.mfa_verify')}
                        </Button>
                        {!enroll && (
                            <button type="button" onClick={() => { setUseRecovery(v => !v); setMfaCode(''); setError(''); setTimeout(() => mfaInputRef.current?.focus(), 50); }}
                                className="w-full text-sm text-muted-foreground hover:text-foreground text-center">
                                {useRecovery ? t('mfa.use_app') : t('mfa.use_recovery')}
                            </button>
                        )}
                        <button type="button" onClick={resetMfa}
                            className="w-full text-xs text-muted-foreground hover:text-foreground text-center transition-colors">
                            ← {t('login.back_to_login')}
                        </button>
                    </form>
                    )}
                </div>
            </div>
        );
    }

    return (
        <div className="flex min-h-screen items-center justify-center bg-background relative overflow-hidden">
            {/* HexOS Honeycomb/Geometric background hint could go here, for now solid background */}
            <div className="w-full max-w-md space-y-8 rounded-2xl border bg-card p-8 shadow-xl relative z-10">
                <div className="text-center">
                    <div className="flex justify-center mb-4">
                        <img src="/avatar.png" alt="OpenEdge" className="h-20 w-20 rounded-2xl object-cover shadow-lg" />
                    </div>
                    <h1 className="text-xl font-semibold tracking-tight">{t('login.banner')}</h1>
                    <p className="mt-1 text-sm text-muted-foreground">{t('login.banner_sub')}</p>
                </div>

                <form className="mt-8 space-y-6" onSubmit={handleSubmit}>
                    <div className="space-y-4">
                        <div className="grid gap-2">
                            <Label htmlFor="username" className="text-xs text-muted-foreground font-medium">{t('login.username')}</Label>
                            <div className="relative">
                                <UserIcon className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
                                <Input
                                    id="username"
                                    type="text"
                                    required
                                    className="pl-10"
                                    placeholder="admin"
                                    value={username}
                                    onChange={(e) => setUsername(e.target.value)}
                                />
                            </div>
                        </div>
                        <div className="grid gap-2">
                            <Label htmlFor="password" className="text-xs text-muted-foreground font-medium">{t('login.password')}</Label>
                            <div className="relative">
                                <Lock className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
                                <Input
                                    id="password"
                                    type={showPassword ? 'text' : 'password'}
                                    required
                                    className="pl-10 pr-10"
                                    placeholder="••••••••"
                                    value={password}
                                    onChange={(e) => setPassword(e.target.value)}
                                />
                                <button
                                    type="button"
                                    onClick={() => setShowPassword(v => !v)}
                                    className="absolute right-3 top-3 text-muted-foreground hover:text-foreground transition-colors"
                                    tabIndex={-1}
                                >
                                    {showPassword
                                        ? <EyeOff className="h-4 w-4" />
                                        : <Eye className="h-4 w-4" />}
                                </button>
                            </div>
                        </div>
                    </div>

                    {error && (
                        <div className="text-xs font-medium text-destructive text-center flex items-center justify-center gap-2 animate-shake">
                            <div className="w-1.5 h-1.5 rounded-full bg-destructive" />
                            {error}
                        </div>
                    )}

                    <Button
                        type="submit"
                        className="w-full flex items-center justify-center gap-2"
                        disabled={loading}
                    >
                        {loading ? (
                            <Loader2 className="h-4 w-4 animate-spin-slow text-primary-foreground" />
                        ) : (
                            <>
                                <span>{t('login.sign_in')}</span>

                            </>
                        )}
                    </Button>
                    <div className="text-center">
                        <Link to="/forgot-password" className="text-xs text-muted-foreground hover:text-foreground transition-colors">
                            {t('login.forgot')}
                        </Link>
                    </div>

                    {/* SSO / Enterprise Login */}
                    <div className="relative">
                        <div className="absolute inset-0 flex items-center">
                            <div className="w-full border-t border-border" />
                        </div>
                        <div className="relative flex justify-center text-xs">
                            <span className="bg-card px-2 text-muted-foreground font-bold">{t('login.sso_divider')}</span>
                        </div>
                    </div>

                    {showSsoInput && (
                        <div className="space-y-2">
                            <Label className="text-xs text-muted-foreground font-medium">{t('login.work_email')}</Label>
                            <div className="relative">
                                <Mail className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
                                <Input
                                    type="email"
                                    placeholder={t('login.work_email_placeholder')}
                                    className="pl-10"
                                    value={ssoEmail}
                                    onChange={(e) => setSsoEmail(e.target.value)}
                                    onKeyDown={(e) => { if (e.key === 'Enter') handleSSOLogin(showSsoInput); }}
                                    autoFocus
                                />
                            </div>
                        </div>
                    )}

                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <Button
                            type="button"
                            variant="outline"
                            className="flex items-center gap-2 text-xs"
                            onClick={() => handleSSOLogin('google')}
                        >
                            <svg className="h-4 w-4" viewBox="0 0 24 24">
                                <path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"/>
                                <path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"/>
                                <path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"/>
                                <path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"/>
                            </svg>
                            Google
                        </Button>
                        <Button
                            type="button"
                            variant="outline"
                            className="flex items-center gap-2 text-xs"
                            onClick={() => handleSSOLogin('azure')}
                        >
                            <svg className="h-4 w-4" viewBox="0 0 23 23">
                                <path fill="#f3f3f3" d="M0 0h23v23H0z"/>
                                <path fill="#f35325" d="M1 1h10v10H1z"/>
                                <path fill="#81bc06" d="M12 1h10v10H12z"/>
                                <path fill="#05a6f0" d="M1 12h10v10H1z"/>
                                <path fill="#ffba08" d="M12 12h10v10H12z"/>
                            </svg>
                            Microsoft
                        </Button>
                    </div>
                </form>
            </div>
        </div>
    );
};

export default LoginPage;
