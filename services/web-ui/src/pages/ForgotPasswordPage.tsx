import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Mail, ArrowLeft, CheckCircle2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { passwordResetApi } from '@/api/passwordReset';

export default function ForgotPasswordPage() {
    const [email, setEmail] = useState('');
    const [loading, setLoading] = useState(false);
    const [sent, setSent] = useState(false);
    const [error, setError] = useState('');

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);
        setError('');
        try {
            await passwordResetApi.forgot(email);
            setSent(true);
        } catch {
            setError('Something went wrong. Please try again.');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="min-h-screen flex items-center justify-center bg-background px-4">
            <div className="w-full max-w-sm space-y-6">
                <div className="flex flex-col items-center gap-2 text-center">
                    <div className="h-12 w-12 rounded-xl bg-primary/10 flex items-center justify-center">
                        <Mail className="h-6 w-6 text-primary" />
                    </div>
                    <h1 className="text-2xl font-bold">Forgot password?</h1>
                    <p className="text-sm text-muted-foreground">
                        Enter your email and we'll send you a reset link.
                    </p>
                </div>

                {sent ? (
                    <div className="rounded-xl border bg-card p-6 text-center space-y-3">
                        <CheckCircle2 className="h-10 w-10 text-green-500 mx-auto" />
                        <p className="font-medium">Check your email</p>
                        <p className="text-sm text-muted-foreground">
                            If <strong>{email}</strong> is registered you'll receive a password reset link within a few minutes.
                        </p>
                        <Link to="/login">
                            <Button variant="outline" className="w-full mt-2">Back to login</Button>
                        </Link>
                    </div>
                ) : (
                    <form onSubmit={handleSubmit} className="rounded-xl border bg-card p-6 space-y-4">
                        <div className="space-y-1.5">
                            <Label htmlFor="email">Email or username</Label>
                            <Input
                                id="email"
                                type="text"
                                placeholder="you@company.com"
                                value={email}
                                onChange={e => setEmail(e.target.value)}
                                required
                                autoFocus
                            />
                        </div>
                        {error && <p className="text-sm text-destructive">{error}</p>}
                        <Button type="submit" className="w-full" disabled={loading || !email}>
                            {loading ? 'Sending…' : 'Send reset link'}
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
