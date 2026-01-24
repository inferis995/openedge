// Design System: See tasks/design-system.md
import { useState, useEffect } from 'react'
import { Plus, Building2 } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useToast } from '@/components/ui/use-toast'
import { createSite, getOrganizations } from '@/services/api'
import type { Organization } from '@/types/api'

interface CreateSiteDialogProps {
  onSuccess?: () => void
}

export function CreateSiteDialog({ onSuccess }: CreateSiteDialogProps) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [orgId, setOrgId] = useState<number | null>(null)
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [isLoadingOrgs, setIsLoadingOrgs] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const { toast } = useToast()

  // Fetch organizations when dialog opens
  useEffect(() => {
    if (open) {
      const fetchOrganizations = async () => {
        setIsLoadingOrgs(true)
        try {
          const orgs = await getOrganizations()
          setOrganizations(orgs)
          // Auto-select first organization if only one exists
          if (orgs.length === 1) {
            setOrgId(orgs[0].id)
          }
        } catch (error) {
          console.error('Failed to fetch organizations:', error)
          toast({
            variant: 'destructive',
            title: 'Error',
            description: 'Failed to load organizations. Please try again.',
          })
        } finally {
          setIsLoadingOrgs(false)
        }
      }

      fetchOrganizations()
    }
  }, [open, toast])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    // Validation: name must not be empty and have at least 2 characters
    if (!name || name.trim().length < 2) {
      toast({
        variant: 'destructive',
        title: 'Invalid Input',
        description: 'Site name must be at least 2 characters long.',
      })
      return
    }

    // Validation: organization must be selected
    if (!orgId) {
      toast({
        variant: 'destructive',
        title: 'Invalid Input',
        description: 'Please select an organization.',
      })
      return
    }

    setIsSubmitting(true)

    try {
      await createSite({ org_id: orgId, name: name.trim() })
      const selectedOrg = organizations.find((org) => org.id === orgId)
      toast({
        title: 'Site Created',
        description: `"${name.trim()}" has been created successfully under ${selectedOrg?.name || 'the organization'}.`,
      })
      setName('')
      setOrgId(null)
      setOpen(false)
      onSuccess?.()
    } catch (error) {
      console.error('Failed to create site:', error)
      const errorMessage = error instanceof Error
        ? error.message
        : 'Failed to create site. Please try again.'
      toast({
        variant: 'destructive',
        title: 'Error',
        description: errorMessage,
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleOpenChange = (newOpen: boolean) => {
    setOpen(newOpen)
    if (!newOpen) {
      // Reset form when dialog closes
      setName('')
      setOrgId(null)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    // Submit on Enter key
    if (e.key === 'Enter' && !isSubmitting) {
      e.preventDefault()
      handleSubmit(e)
    }
    // Close on Escape key
    if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  const selectedOrg = organizations.find((org) => org.id === orgId)

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button variant="default" className="gap-2">
          <Plus className="h-4 w-4" />
          New Site
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-[425px]" onKeyDown={handleKeyDown}>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Building2 className="h-5 w-5 text-violet-600" />
              Create Site
              {selectedOrg && (
                <span className="text-sm font-normal text-muted-foreground">
                  for {selectedOrg.name}
                </span>
              )}
            </DialogTitle>
            <DialogDescription>
              Create a new site within an organization to organize your locations.
              Sites represent physical facilities or campuses in your hierarchy.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            {/* Organization Dropdown */}
            <div className="grid gap-2">
              <label htmlFor="org-select" className="text-sm font-medium">
                Organization *
              </label>
              <Select
                value={orgId?.toString() || ''}
                onValueChange={(value) => setOrgId(Number(value))}
                disabled={isLoadingOrgs || isSubmitting}
              >
                <SelectTrigger id="org-select">
                  {isLoadingOrgs ? (
                    <SelectValue placeholder="Loading organizations..." />
                  ) : (
                    <SelectValue placeholder="Select an organization" />
                  )}
                </SelectTrigger>
                <SelectContent>
                  {organizations.map((org) => (
                    <SelectItem key={org.id} value={org.id.toString()}>
                      {org.name}
                    </SelectItem>
                  ))}
                  {organizations.length === 0 && !isLoadingOrgs && (
                    <div className="px-2 py-1.5 text-sm text-muted-foreground">
                      No organizations available
                    </div>
                  )}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                Select the parent organization for this site.
              </p>
            </div>

            {/* Site Name Input */}
            <div className="grid gap-2">
              <label htmlFor="site-name" className="text-sm font-medium">
                Site Name *
              </label>
              <Input
                id="site-name"
                placeholder="e.g., Factory A, Warehouse 1"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={isSubmitting}
                autoFocus
                className="col-span-3"
              />
              <p className="text-xs text-muted-foreground">
                Must be at least 2 characters long.
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={isSubmitting}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={isSubmitting || !name.trim() || !orgId}
            >
              {isSubmitting ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
