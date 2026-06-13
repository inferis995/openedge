import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { KeyRound, ArrowLeft, CheckCircle2, Eye, EyeOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { passwordResetApi } from '@/api/passwordReset';

export default function ResetPasswordPage() {
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
            setError('Passwords do not match.');
            return;
        }
        if (password.length < 6) {
            setError('Password must be at least 6 characters.');
            return;
        }
        setLoading(true);
        setError('');
        try {
            await passwordResetApi.reset(token, password);
            setDone(true);
        } catch (err: unknown) {
            const axiosErr = err as { response?: { data?: { error?: string } } };
            setError(axiosErr.response?.data?.error ?? 'Reset failed. The link may have expired.');
        } finally {
            setLoading(false);
        }
    };

    if (!token) {
        return (
            <div className="min-h-screen flex items-center justify-center bg-background px-4">
                <div className="text-center space-y-3">
                    <p className="text-destructive font-medium">Invalid reset link.</p>
                    <Link to="/forgot-password">
                        <Button variant="outline">Request a new one</Button>
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
                        <KeyRound className="h-6 w-6 text-primary" />
                    </div>
                    <h1 className="text-2xl font-bold">Set new password</h1>
                    <p className="text-sm text-muted-foreground">
                        Choose a strong password for your account.
                    </p>
                </div>

                {done ? (
                    <div className="rounded-xl border bg-card p-6 text-center space-y-3">
                        <CheckCircle2 className="h-10 w-10 text-green-500 mx-auto" />
                        <p className="font-medium">Password updated!</p>
                        <p className="text-sm text-muted-foreground">You can now log in with your new password.</p>
                        <Link to="/login">
                            <Button className="w-full mt-2">Go to login</Button>
                        </Link>
                    </div>
                ) : (
                    <form onSubmit={handleSubmit} className="rounded-xl border bg-card p-6 space-y-4">
                        <div className="space-y-1.5">
                            <Label htmlFor="password">New password</Label>
                            <div className="relative">
                                <Input
                                    id="password"
                                    type={showPw ? 'text' : 'password'}
                                    placeholder="Min. 6 characters"
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
                            <Label htmlFor="confirm">Confirm password</Label>
                            <Input
                                id="confirm"
                                type="password"
                                placeholder="Repeat password"
                                value={confirm}
                                onChange={e => setConfirm(e.target.value)}
                                required
                            />
                        </div>
                        {error && <p className="text-sm text-destructive">{error}</p>}
                        <Button type="submit" className="w-full" disabled={loading || !password || !confirm}>
                            {loading ? 'Updating…' : 'Update password'}
                        </Button>
                    </form>
                )}

                <Link to="/login" className="flex items-center justify-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors">
                    <ArrowLeft className="h-4 w-4" /> Back to login
                </Link>
            </div>
        </div>
    );
}
