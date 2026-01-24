// Design System: See tasks/design-system.md
import { useState, useEffect } from 'react'
import { Plus, Map } from 'lucide-react'
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
import { createArea, getSites } from '@/services/api'
import type { Site } from '@/types/api'

interface CreateAreaDialogProps {
  onSuccess?: () => void
}

export function CreateAreaDialog({ onSuccess }: CreateAreaDialogProps) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [siteId, setSiteId] = useState<number | null>(null)
  const [sites, setSites] = useState<Site[]>([])
  const [isLoadingSites, setIsLoadingSites] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const { toast } = useToast()

  // Fetch sites when dialog opens
  useEffect(() => {
    if (open) {
      const fetchSites = async () => {
        setIsLoadingSites(true)
        try {
          const sitesList = await getSites()
          setSites(sitesList)
          // Auto-select first site if only one exists
          if (sitesList.length === 1) {
            setSiteId(sitesList[0].id)
          }
        } catch (error) {
          console.error('Failed to fetch sites:', error)
          toast({
            variant: 'destructive',
            title: 'Error',
            description: 'Failed to load sites. Please try again.',
          })
        } finally {
          setIsLoadingSites(false)
        }
      }

      fetchSites()
    }
  }, [open, toast])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    // Validation: name must not be empty and have at least 2 characters
    if (!name || name.trim().length < 2) {
      toast({
        variant: 'destructive',
        title: 'Invalid Input',
        description: 'Area name must be at least 2 characters long.',
      })
      return
    }

    // Validation: site must be selected
    if (!siteId) {
      toast({
        variant: 'destructive',
        title: 'Invalid Input',
        description: 'Please select a parent site.',
      })
      return
    }

    setIsSubmitting(true)

    try {
      await createArea({ site_id: siteId, name: name.trim() })
      const selectedSite = sites.find((site) => site.id === siteId)
      toast({
        title: 'Area Created',
        description: `"${name.trim()}" has been created successfully under ${selectedSite?.name || 'the site'}.`,
      })
      setName('')
      setSiteId(null)
      setOpen(false)
      onSuccess?.()
    } catch (error) {
      console.error('Failed to create area:', error)
      const errorMessage = error instanceof Error
        ? error.message
        : 'Failed to create area. Please try again.'
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
      setSiteId(null)
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

  const selectedSite = sites.find((site) => site.id === siteId)

  // Build breadcrumb: Org > Site > New Area
  const breadcrumb = selectedSite
    ? `Organization > ${selectedSite.name} > New Area`
    : 'Organization > [Select Site] > New Area'

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button variant="default" className="gap-2">
          <Plus className="h-4 w-4" />
          New Area
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-[425px]" onKeyDown={handleKeyDown}>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Map className="h-5 w-5 text-violet-600" />
              Create Area
            </DialogTitle>
            <DialogDescription>
              <div className="flex flex-col gap-1">
                <p>{breadcrumb}</p>
                <p className="text-muted-foreground">
                  Create a new area within a site to define physical zones.
                </p>
              </div>
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            {/* Site Dropdown */}
            <div className="grid gap-2">
              <label htmlFor="site-select" className="text-sm font-medium">
                Parent Site *
              </label>
              <Select
                value={siteId?.toString() || ''}
                onValueChange={(value) => setSiteId(Number(value))}
                disabled={isLoadingSites || isSubmitting}
              >
                <SelectTrigger id="site-select">
                  {isLoadingSites ? (
                    <SelectValue placeholder="Loading sites..." />
                  ) : (
                    <SelectValue placeholder="Select a parent site" />
                  )}
                </SelectTrigger>
                <SelectContent>
                  {sites.map((site) => (
                    <SelectItem key={site.id} value={site.id.toString()}>
                      {site.name}
                    </SelectItem>
                  ))}
                  {sites.length === 0 && !isLoadingSites && (
                    <div className="px-2 py-1.5 text-sm text-muted-foreground">
                      No sites available
                    </div>
                  )}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                Sites are filtered by your current organization.
              </p>
            </div>

            {/* Area Name Input */}
            <div className="grid gap-2">
              <label htmlFor="area-name" className="text-sm font-medium">
                Area Name *
              </label>
              <Input
                id="area-name"
                placeholder="e.g., Production Line, Warehouse Zone A"
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
              disabled={isSubmitting || !name.trim() || !siteId}
            >
              {isSubmitting ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
