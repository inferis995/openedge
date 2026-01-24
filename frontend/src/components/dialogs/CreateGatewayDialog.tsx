// Design System: See tasks/design-system.md
import { useState, useEffect } from 'react'
import { Plus, Server, Network } from 'lucide-react'
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
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useToast } from '@/components/ui/use-toast'
import { createGateway, getAreas } from '@/services/api'
import type { Area } from '@/types/api'

interface CreateGatewayDialogProps {
  onSuccess?: () => void
}

interface ConnectionConfig {
  // S7 fields
  ip?: string
  rack?: number
  slot?: number
  port?: number
  // Modbus fields
  slaveId?: number
}

export function CreateGatewayDialog({ onSuccess }: CreateGatewayDialogProps) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [driverType, setDriverType] = useState<'S7' | 'MODBUS_TCP'>('S7')
  const [areaId, setAreaId] = useState<number | null>(null)
  const [areas, setAreas] = useState<Area[]>([])
  const [isLoadingAreas, setIsLoadingAreas] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)

  // Connection config fields
  const [ip, setIp] = useState('')
  const [rack, setRack] = useState(0)
  const [slot, setSlot] = useState(1)
  const [port, setPort] = useState(102)
  const [slaveId, setSlaveId] = useState(1)
  const [scanRateMs, setScanRateMs] = useState(1000)
  const [enabled, setEnabled] = useState(true)

  const { toast } = useToast()

  // Fetch areas when dialog opens
  useEffect(() => {
    if (open) {
      const fetchAreas = async () => {
        setIsLoadingAreas(true)
        try {
          // Fetch all areas for the organization (no site_id filter)
          const areasList = await getAreas()
          setAreas(areasList)
          // Auto-select first area if only one exists
          if (areasList.length === 1) {
            setAreaId(areasList[0].id)
          }
        } catch (error) {
          console.error('Failed to fetch areas:', error)
          toast({
            variant: 'destructive',
            title: 'Error',
            description: 'Failed to load areas. Please try again.',
          })
        } finally {
          setIsLoadingAreas(false)
        }
      }

      fetchAreas()
    }
  }, [open, toast])

  const buildConnectionConfig = (): ConnectionConfig => {
    if (driverType === 'S7') {
      return {
        ip: ip.trim(),
        rack,
        slot,
        port,
      }
    } else {
      return {
        ip: ip.trim(),
        port,
        slaveId,
      }
    }
  }

  const validateConfig = (): boolean => {
    // Validate IP address format
    const ipRegex = /^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$/
    if (!ip.trim() || !ipRegex.test(ip.trim())) {
      toast({
        variant: 'destructive',
        title: 'Invalid IP Address',
        description: 'Please enter a valid IPv4 address (e.g., 192.168.1.100)',
      })
      return false
    }

    // Validate port ranges
    if (driverType === 'S7') {
      if (port < 1 || port > 65535) {
        toast({
          variant: 'destructive',
          title: 'Invalid Port',
          description: 'S7 port must be between 1 and 65535 (default: 102)',
        })
        return false
      }
      if (rack < 0 || rack > 7) {
        toast({
          variant: 'destructive',
          title: 'Invalid Rack',
          description: 'Rack must be between 0 and 7',
        })
        return false
      }
      if (slot < 0 || slot > 31) {
        toast({
          variant: 'destructive',
          title: 'Invalid Slot',
          description: 'Slot must be between 0 and 31',
        })
        return false
      }
    } else {
      if (port < 1 || port > 65535) {
        toast({
          variant: 'destructive',
          title: 'Invalid Port',
          description: 'Modbus port must be between 1 and 65535 (default: 502)',
        })
        return false
      }
      if (slaveId < 1 || slaveId > 247) {
        toast({
          variant: 'destructive',
          title: 'Invalid Slave ID',
          description: 'Slave ID must be between 1 and 247',
        })
        return false
      }
    }

    // Validate scan rate
    if (scanRateMs < 100 || scanRateMs > 60000) {
      toast({
        variant: 'destructive',
        title: 'Invalid Scan Rate',
        description: 'Scan rate must be between 100 and 60000 ms',
      })
      return false
    }

    return true
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    // Validation: name must not be empty and have at least 2 characters
    if (!name || name.trim().length < 2) {
      toast({
        variant: 'destructive',
        title: 'Invalid Input',
        description: 'Gateway name must be at least 2 characters long.',
      })
      return
    }

    // Validation: area must be selected
    if (!areaId) {
      toast({
        variant: 'destructive',
        title: 'Invalid Input',
        description: 'Please select a parent area.',
      })
      return
    }

    // Validate connection config
    if (!validateConfig()) {
      return
    }

    setIsSubmitting(true)

    try {
      await createGateway({
        area_id: areaId,
        name: name.trim(),
        driver_type: driverType,
        connection_config: buildConnectionConfig() as any,
        scan_rate_ms: scanRateMs,
        enabled,
      })

      const selectedArea = areas.find((area) => area.id === areaId)
      toast({
        title: 'Gateway Created',
        description: `"${name.trim()}" (${driverType}) has been created successfully under ${selectedArea?.name || 'the area'}.`,
      })

      // Reset form
      setName('')
      setDriverType('S7')
      setAreaId(null)
      setIp('')
      setRack(0)
      setSlot(1)
      setPort(102)
      setSlaveId(1)
      setScanRateMs(1000)
      setEnabled(true)

      setOpen(false)
      onSuccess?.()
    } catch (error) {
      console.error('Failed to create gateway:', error)
      const errorMessage = error instanceof Error
        ? error.message
        : 'Failed to create gateway. Please try again.'
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
      setDriverType('S7')
      setAreaId(null)
      setIp('')
      setRack(0)
      setSlot(1)
      setPort(102)
      setSlaveId(1)
      setScanRateMs(1000)
      setEnabled(true)
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

  const handleDriverTypeChange = (value: string) => {
    setDriverType(value as 'S7' | 'MODBUS_TCP')
    // Reset driver-specific defaults
    if (value === 'S7') {
      setPort(102)
    } else {
      setPort(502)
    }
  }

  const selectedArea = areas.find((area) => area.id === areaId)

  // Build connection config preview JSON
  const configPreview = JSON.stringify(buildConnectionConfig(), null, 2)

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button variant="default" className="gap-2">
          <Plus className="h-4 w-4" />
          New Gateway
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-[500px] max-h-[90vh] overflow-y-auto" onKeyDown={handleKeyDown}>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Server className="h-5 w-5 text-violet-600" />
              Create Gateway
            </DialogTitle>
            <DialogDescription>
              <div className="flex flex-col gap-1">
                <p>
                  {selectedArea
                    ? `Organization > ... > ${selectedArea.name} > New Gateway`
                    : 'Organization > ... > [Select Area] > New Gateway'}
                </p>
                <p className="text-muted-foreground">
                  Create a new gateway to connect PLC drivers (S7 or Modbus TCP).
                </p>
              </div>
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-4">
            {/* Area Dropdown */}
            <div className="grid gap-2">
              <label htmlFor="area-select" className="text-sm font-medium">
                Parent Area *
              </label>
              <Select
                value={areaId?.toString() || ''}
                onValueChange={(value) => setAreaId(Number(value))}
                disabled={isLoadingAreas || isSubmitting}
              >
                <SelectTrigger id="area-select">
                  {isLoadingAreas ? (
                    <SelectValue placeholder="Loading areas..." />
                  ) : (
                    <SelectValue placeholder="Select a parent area" />
                  )}
                </SelectTrigger>
                <SelectContent>
                  {areas.map((area) => (
                    <SelectItem key={area.id} value={area.id.toString()}>
                      {area.name}
                    </SelectItem>
                  ))}
                  {areas.length === 0 && !isLoadingAreas && (
                    <div className="px-2 py-1.5 text-sm text-muted-foreground">
                      No areas available
                    </div>
                  )}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                Areas are filtered by your current organization.
              </p>
            </div>

            {/* Gateway Name */}
            <div className="grid gap-2">
              <label htmlFor="gateway-name" className="text-sm font-medium">
                Gateway Name *
              </label>
              <Input
                id="gateway-name"
                placeholder="e.g., PLC-01, Modbus-Gateway-A"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={isSubmitting}
                autoFocus
              />
              <p className="text-xs text-muted-foreground">
                Must be at least 2 characters long.
              </p>
            </div>

            {/* Driver Type */}
            <div className="grid gap-2">
              <label htmlFor="driver-type" className="text-sm font-medium">
                Driver Type *
              </label>
              <Select
                value={driverType}
                onValueChange={handleDriverTypeChange}
                disabled={isSubmitting}
              >
                <SelectTrigger id="driver-type">
                  <SelectValue placeholder="Select driver type" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="S7">Siemens S7</SelectItem>
                  <SelectItem value="MODBUS_TCP">Modbus TCP</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="border-t pt-4">
              <div className="flex items-center gap-2 mb-3">
                <Network className="h-4 w-4 text-violet-600" />
                <h3 className="text-sm font-semibold">Connection Configuration</h3>
              </div>

              {/* IP Address (common for both) */}
              <div className="grid gap-2 mb-3">
                <label htmlFor="ip-address" className="text-sm font-medium">
                  IP Address *
                </label>
                <Input
                  id="ip-address"
                  placeholder="192.168.1.100"
                  value={ip}
                  onChange={(e) => setIp(e.target.value)}
                  disabled={isSubmitting}
                />
              </div>

              {/* S7-specific fields */}
              {driverType === 'S7' && (
                <>
                  <div className="grid grid-cols-2 gap-3 mb-3">
                    <div className="grid gap-2">
                      <label htmlFor="rack" className="text-sm font-medium">
                        Rack
                      </label>
                      <Input
                        id="rack"
                        type="number"
                        min="0"
                        max="7"
                        value={rack}
                        onChange={(e) => setRack(Number(e.target.value))}
                        disabled={isSubmitting}
                      />
                      <p className="text-xs text-muted-foreground">0-7</p>
                    </div>
                    <div className="grid gap-2">
                      <label htmlFor="slot" className="text-sm font-medium">
                        Slot
                      </label>
                      <Input
                        id="slot"
                        type="number"
                        min="0"
                        max="31"
                        value={slot}
                        onChange={(e) => setSlot(Number(e.target.value))}
                        disabled={isSubmitting}
                      />
                      <p className="text-xs text-muted-foreground">0-31</p>
                    </div>
                  </div>
                  <div className="grid gap-2 mb-3">
                    <label htmlFor="s7-port" className="text-sm font-medium">
                      Port
                    </label>
                    <Input
                      id="s7-port"
                      type="number"
                      min="1"
                      max="65535"
                      value={port}
                      onChange={(e) => setPort(Number(e.target.value))}
                      disabled={isSubmitting}
                    />
                    <p className="text-xs text-muted-foreground">
                      Default: 102 (S7 standard port)
                    </p>
                  </div>
                </>
              )}

              {/* Modbus-specific fields */}
              {driverType === 'MODBUS_TCP' && (
                <>
                  <div className="grid grid-cols-2 gap-3 mb-3">
                    <div className="grid gap-2">
                      <label htmlFor="modbus-port" className="text-sm font-medium">
                        Port
                      </label>
                      <Input
                        id="modbus-port"
                        type="number"
                        min="1"
                        max="65535"
                        value={port}
                        onChange={(e) => setPort(Number(e.target.value))}
                        disabled={isSubmitting}
                      />
                      <p className="text-xs text-muted-foreground">
                        Default: 502 (Modbus TCP)
                      </p>
                    </div>
                    <div className="grid gap-2">
                      <label htmlFor="slave-id" className="text-sm font-medium">
                        Slave ID
                      </label>
                      <Input
                        id="slave-id"
                        type="number"
                        min="1"
                        max="247"
                        value={slaveId}
                        onChange={(e) => setSlaveId(Number(e.target.value))}
                        disabled={isSubmitting}
                      />
                      <p className="text-xs text-muted-foreground">1-247</p>
                    </div>
                  </div>
                </>
              )}
            </div>

            {/* JSON Preview */}
            <div className="grid gap-2">
              <label className="text-sm font-medium">JSON Preview</label>
              <pre className="bg-muted p-3 rounded text-xs overflow-x-auto">
                {configPreview}
              </pre>
            </div>

            <div className="border-t pt-4">
              <div className="grid gap-3">
                {/* Scan Rate */}
                <div className="grid gap-2">
                  <label htmlFor="scan-rate" className="text-sm font-medium">
                    Scan Rate (ms)
                  </label>
                  <Input
                    id="scan-rate"
                    type="number"
                    min="100"
                    max="60000"
                    step="100"
                    value={scanRateMs}
                    onChange={(e) => setScanRateMs(Number(e.target.value))}
                    disabled={isSubmitting}
                  />
                  <p className="text-xs text-muted-foreground">
                    How often to poll the PLC (100-60000 ms, default: 1000)
                  </p>
                </div>

                {/* Enabled Toggle */}
                <div className="flex items-center justify-between">
                  <div className="grid gap-1">
                    <label htmlFor="enabled" className="text-sm font-medium">
                      Enabled
                    </label>
                    <p className="text-xs text-muted-foreground">
                      Start the driver container immediately
                    </p>
                  </div>
                  <Switch
                    id="enabled"
                    checked={enabled}
                    onCheckedChange={setEnabled}
                    disabled={isSubmitting}
                  />
                </div>
              </div>
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
              disabled={isSubmitting || !name.trim() || !areaId || !ip.trim()}
            >
              {isSubmitting ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
