import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/stores/useAuthStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Lock, User as UserIcon, Loader2 } from 'lucide-react';

const LoginPage = () => {
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);
    const navigate = useNavigate();
    const { login } = useAuthStore();

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
                throw new Error('Invalid credentials');
            }

            const data = await response.json();
            login(data.token, data.user);
            navigate('/');
        } catch (err) {
            setError('Invalid username or password');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="flex min-h-screen items-center justify-center bg-background relative overflow-hidden">
            {/* HexOS Honeycomb/Geometric background hint could go here, for now solid background */}
            <div className="w-full max-w-md space-y-8 clip-chamfer border bg-card p-8 shadow-2xl relative z-10">
                <div className="text-center">
                    <div className="flex justify-center mb-6">
                        <img src="/logo-dark.png" alt="OpenEdge" className="h-20 w-auto object-contain hidden dark:block" />
                        <img src="/logo-light.png" alt="OpenEdge" className="h-20 w-auto object-contain dark:hidden" />
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
                                    type="password"
                                    required
                                    className="pl-10"
                                    placeholder="••••••••"
                                    value={password}
                                    onChange={(e) => setPassword(e.target.value)}
                                />
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
                </form>
            </div>
        </div>
    );
};

export default LoginPage;
