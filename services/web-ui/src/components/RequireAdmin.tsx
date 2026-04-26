import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { useAuthStore } from '@/stores/useAuthStore';

const RequireAdmin = () => {
    const { isAuthenticated, isAdmin } = useAuthStore();
    const location = useLocation();

    if (!isAuthenticated()) {
        return <Navigate to="/login" state={{ from: location }} replace />;
    }
    if (!isAdmin()) {
        return <Navigate to="/" replace />;
    }
    return <Outlet />;
};

export default RequireAdmin;
