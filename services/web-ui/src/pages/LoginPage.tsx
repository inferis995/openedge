import { useState, useEffect, useRef } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuthStore } from '@/stores/useAuthStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Lock, User as UserIcon, Loader2, Mail, Eye, EyeOff, ShieldCheck } from 'lucide-react';

const LoginPage = () => {
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
                throw new Error(body.error || 'Invalid credentials');
            }

            const data = await response.json();

            if (data.mfa_required) {
                setMfaToken(data.mfa_token);
                setMfaStep(true);
                setTimeout(() => mfaInputRef.current?.focus(), 100);
                return;
            }

            login(data.token, data.user);
            navigate('/');
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : 'Invalid username or password');
        } finally {
            setLoading(false);
        }
    };

    const handleMFASubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError('');
        setLoading(true);
        try {
            const response = await fetch('/api/auth/mfa/verify', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ mfa_token: mfaToken, code: mfaCode }),
            });
            if (!response.ok) {
                const body = await response.json().catch(() => ({}));
                throw new Error(body.error || 'Codice non valido');
            }
            const data = await response.json();
            login(data.token, data.user);
            navigate('/');
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : 'Codice non valido');
            setMfaCode('');
            mfaInputRef.current?.focus();
        } finally {
            setLoading(false);
        }
    };

    if (mfaStep) {
        return (
            <div className="flex min-h-screen items-center justify-center bg-background">
                <div className="w-full max-w-sm space-y-8 clip-chamfer border bg-card p-8 shadow-2xl">
                    <div className="text-center">
                        <div className="flex justify-center mb-4">
                            <div className="h-16 w-16 rounded-2xl bg-primary/10 flex items-center justify-center">
                                <ShieldCheck className="h-8 w-8 text-primary" />
                            </div>
                        </div>
                        <h2 className="text-lg font-semibold">Autenticazione a due fattori</h2>
                        <p className="mt-1 text-xs text-muted-foreground">Inserisci il codice a 6 cifre dall'app autenticatore</p>
                    </div>
                    <form className="space-y-6" onSubmit={handleMFASubmit}>
                        <div className="grid gap-2">
                            <Label className="uppercase text-[10px] tracking-widest text-muted-foreground font-bold">Codice OTP</Label>
                            <Input
                                ref={mfaInputRef}
                                type="text"
                                inputMode="numeric"
                                pattern="[0-9]{6}"
                                maxLength={6}
                                placeholder="000000"
                                value={mfaCode}
                                onChange={e => setMfaCode(e.target.value.replace(/\D/g, ''))}
                                className="text-center text-2xl tracking-widest font-mono"
                                autoComplete="one-time-code"
                            />
                        </div>
                        {error && (
                            <p className="text-[10px] font-bold tracking-widest uppercase text-destructive text-center">{error}</p>
                        )}
                        <Button type="submit" className="w-full" disabled={loading || mfaCode.length !== 6}>
                            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Verifica codice'}
                        </Button>
                        <button type="button" onClick={() => { setMfaStep(false); setMfaCode(''); setError(''); }}
                            className="w-full text-xs text-muted-foreground hover:text-foreground text-center transition-colors">
                            ← Torna al login
                        </button>
                    </form>
                </div>
            </div>
        );
    }

    return (
        <div className="flex min-h-screen items-center justify-center bg-background relative overflow-hidden">
            {/* HexOS Honeycomb/Geometric background hint could go here, for now solid background */}
            <div className="w-full max-w-md space-y-8 clip-chamfer border bg-card p-8 shadow-2xl relative z-10">
                <div className="text-center">
                    <div className="flex justify-center mb-4">
                        <img src="/avatar.png" alt="OpenEdge" className="h-20 w-20 rounded-2xl object-cover shadow-lg" />
                    </div>
                    <p className="mt-2 text-sm text-muted-foreground font-mono tracking-widest uppercase">
                        System_Login
                    </p>
                </div>

                <form className="mt-8 space-y-6" onSubmit={handleSubmit}>
                    <div className="space-y-4">
                        <div className="grid gap-2">
                            <Label htmlFor="username" className="uppercase text-[10px] tracking-widest text-muted-foreground font-bold">Username</Label>
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
                            <Label htmlFor="password" className="uppercase text-[10px] tracking-widest text-muted-foreground font-bold">Password</Label>
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
                        <div className="text-[10px] font-bold tracking-widest uppercase text-destructive text-center flex items-center justify-center gap-2 animate-shake">
                            <div className="w-1.5 h-1.5 clip-hex bg-destructive" />
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
                                <span>Sign In _</span>
                                <div className="w-1.5 h-1.5 bg-primary-foreground clip-hex" />
                            </>
                        )}
                    </Button>
                    <div className="text-center">
                        <Link to="/forgot-password" className="text-xs text-muted-foreground hover:text-foreground transition-colors">
                            Forgot your password?
                        </Link>
                    </div>

                    {/* SSO / Enterprise Login */}
                    <div className="relative">
                        <div className="absolute inset-0 flex items-center">
                            <div className="w-full border-t border-border" />
                        </div>
                        <div className="relative flex justify-center text-[10px] uppercase tracking-widest">
                            <span className="bg-card px-2 text-muted-foreground font-bold">or enterprise sso</span>
                        </div>
                    </div>

                    {showSsoInput && (
                        <div className="space-y-2">
                            <Label className="uppercase text-[10px] tracking-widest text-muted-foreground font-bold">Work Email</Label>
                            <div className="relative">
                                <Mail className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
                                <Input
                                    type="email"
                                    placeholder="you@company.com"
                                    className="pl-10"
                                    value={ssoEmail}
                                    onChange={(e) => setSsoEmail(e.target.value)}
                                    onKeyDown={(e) => { if (e.key === 'Enter') handleSSOLogin(showSsoInput); }}
                                    autoFocus
                                />
                            </div>
                        </div>
                    )}

                    <div className="grid grid-cols-2 gap-3">
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
