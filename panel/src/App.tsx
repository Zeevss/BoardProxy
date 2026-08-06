import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { Unauthorized } from "@/lib/api";
import { AuthProvider, notifyUnauthorized } from "@/lib/auth";
import { Toaster } from "@/components/ui/sonner";
import { AppLayout } from "@/components/AppLayout";
import { RequireAuth } from "@/components/RequireAuth";
import { Login } from "@/routes/Login";
import { Dashboard } from "@/routes/Dashboard";
import { Clients } from "@/routes/Clients";
import { Boards } from "@/routes/Boards";
import { Logs } from "@/routes/Logs";
import { Maintenance } from "@/routes/Maintenance";
import { Statistics } from "@/routes/Statistics";
import { Nodes } from "@/routes/Nodes";

const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (err) => {
      if (err instanceof Unauthorized) notifyUnauthorized();
    },
  }),
  defaultOptions: {
    queries: { retry: false, refetchOnWindowFocus: false, staleTime: 5_000 },
  },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route
              element={
                <RequireAuth>
                  <AppLayout />
                </RequireAuth>
              }
            >
              <Route index element={<Dashboard />} />
			  <Route path="nodes" element={<Nodes />} />
              <Route path="clients" element={<Clients />} />
              <Route path="boards" element={<Boards />} />
              <Route path="statistics" element={<Statistics />} />
              <Route path="logs" element={<Logs />} />
              <Route path="maintenance" element={<Maintenance />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </AuthProvider>
        <Toaster />
      </BrowserRouter>
    </QueryClientProvider>
  );
}
