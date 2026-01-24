import { useState } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select';
import { Badge } from './ui/badge';
import { Separator } from './ui/separator';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from './ui/alert-dialog';
import { useToast } from './ui/use-toast';
import type { TreeNode } from '@/services/hierarchy';
import { updateGateway, updateTag } from '@/services/api';
import { Loader2, Pencil, X, Save, Trash2 } from 'lucide-react';

interface DetailPanelProps {
  node: TreeNode | null;
  onUpdate?: () => void;
  className?: string;
}

interface FormData {
  [key: string]: string | number | boolean | object;
}

export function DetailPanel({ node, onUpdate, className }: DetailPanelProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [formData, setFormData] = useState<FormData>({});
  const { toast } = useToast();

  if (!node) {
    return (
      <Card className={className}>
        <CardContent className="flex items-center justify-center h-full min-h-[400px]">
          <p className="text-gray-500 text-center">Select an item from the tree to view details</p>
        </CardContent>
      </Card>
    );
  }

  const handleEdit = () => {
    setFormData(extractFormData(node));
    setIsEditing(true);
  };

  const handleCancel = () => {
    setIsEditing(false);
    setFormData({});
  };

  const handleSave = async () => {
    setIsLoading(true);
    try {
      switch (node.type) {
        case 'gateway':
          await updateGateway(node.id, formData);
          break;
        case 'tag':
          await updateTag(node.id, formData);
          break;
        default:
          throw new Error(`Cannot update ${node.type}`);
      }
      toast({
        title: 'Success',
        description: `${node.type.charAt(0).toUpperCase() + node.type.slice(1)} updated successfully`,
      });
      setIsEditing(false);
      if (onUpdate) onUpdate();
    } catch (error) {
      toast({
        title: 'Error',
        description: error instanceof Error ? error.message : 'Failed to update',
        variant: 'destructive',
      });
    } finally {
      setIsLoading(false);
    }
  };

  const handleDelete = async () => {
    setIsLoading(true);
    try {
      // Note: Delete endpoints not implemented in backend yet
      toast({
        title: 'Not Implemented',
        description: 'Delete functionality will be implemented in the backend',
        variant: 'destructive',
      });
      setShowDeleteDialog(false);
    } catch (error) {
      toast({
        title: 'Error',
        description: error instanceof Error ? error.message : 'Failed to delete',
        variant: 'destructive',
      });
    } finally {
      setIsLoading(false);
    }
  };

  const canEdit = node.type === 'gateway' || node.type === 'tag';
  const canDelete = false; // Backend delete endpoints not implemented

  return (
    <>
      <Card className={className}>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-2">
                {node.name}
                <Badge variant="outline">{node.type}</Badge>
              </CardTitle>
              <CardDescription>ID: {node.id}</CardDescription>
            </div>
            <div className="flex gap-2">
              {canEdit && !isEditing && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleEdit}
                >
                  <Pencil className="h-4 w-4" />
                </Button>
              )}
              {canDelete && !isEditing && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowDeleteDialog(true)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              )}
            </div>
          </div>
        </CardHeader>
        <Separator />
        <CardContent className="pt-6">
          {isEditing ? (
            <EditForm
              node={node}
              formData={formData}
              onChange={setFormData}
              onSave={handleSave}
              onCancel={handleCancel}
              isLoading={isLoading}
            />
          ) : (
            <ViewMode node={node} />
          )}
        </CardContent>
      </Card>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {node.type}?</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete &quot;{node.name}&quot;? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={isLoading}>
              {isLoading ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Deleting...
                </>
              ) : (
                'Delete'
              )}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function ViewMode({ node }: { node: TreeNode }) {
  const fields = getDisplayFields(node);

  return (
    <div className="space-y-4">
      {fields.map(([label, value]) => (
        <div key={label} className="grid grid-cols-[150px_1fr] gap-4">
          <div className="text-sm font-medium text-gray-500">{label}</div>
          <div className="text-sm">{formatValue(value)}</div>
        </div>
      ))}
    </div>
  );
}

interface EditFormProps {
  node: TreeNode;
  formData: FormData;
  onChange: (data: FormData) => void;
  onSave: () => void;
  onCancel: () => void;
  isLoading: boolean;
}

function EditForm({ node, formData, onChange, onSave, onCancel, isLoading }: EditFormProps) {
  const fields = getEditableFields(node);

  const handleFieldChange = (key: string, value: string | number | boolean) => {
    onChange({ ...formData, [key]: value });
  };

  return (
    <div className="space-y-4">
      {fields.map(([label, key, type, options]) => (
        <div key={key} className="space-y-2">
          <label className="text-sm font-medium">{label}</label>
          {type === 'select' ? (
            <Select
              value={String(formData[key] || '')}
              onValueChange={(value) => handleFieldChange(key, value)}
            >
              <SelectTrigger>
                <SelectValue placeholder={`Select ${label.toLowerCase()}`} />
              </SelectTrigger>
              <SelectContent>
                {options?.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : type === 'checkbox' ? (
            <div className="flex items-center space-x-2">
              <input
                type="checkbox"
                id={key}
                checked={Boolean(formData[key])}
                onChange={(e) => handleFieldChange(key, e.target.checked)}
                className="h-4 w-4"
              />
              <label htmlFor={key} className="text-sm">
                {formData[key] ? 'Enabled' : 'Disabled'}
              </label>
            </div>
          ) : type === 'number' ? (
            <Input
              type="number"
              value={String(formData[key] || '')}
              onChange={(e) => handleFieldChange(key, parseFloat(e.target.value) || 0)}
            />
          ) : type === 'json' ? (
            <textarea
              className="w-full min-h-[100px] px-3 py-2 text-sm border rounded-md"
              value={JSON.stringify(formData[key] || {}, null, 2)}
              onChange={(e) => {
                try {
                  handleFieldChange(key, JSON.parse(e.target.value));
                } catch {
                  // Invalid JSON, don't update
                }
              }}
            />
          ) : (
            <Input
              type="text"
              value={String(formData[key] || '')}
              onChange={(e) => handleFieldChange(key, e.target.value)}
            />
          )}
        </div>
      ))}

      <div className="flex gap-2 pt-4">
        <Button onClick={onSave} disabled={isLoading}>
          {isLoading ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Saving...
            </>
          ) : (
            <>
              <Save className="mr-2 h-4 w-4" />
              Save
            </>
          )}
        </Button>
        <Button variant="outline" onClick={onCancel} disabled={isLoading}>
          <X className="mr-2 h-4 w-4" />
          Cancel
        </Button>
      </div>
    </div>
  );
}

function extractFormData(node: TreeNode): FormData {
  const data: FormData = {};

  switch (node.type) {
    case 'gateway':
      const gw = node.data as any;
      data.name = gw.name;
      data.driver_type = gw.driver_type;
      data.scan_rate_ms = gw.scan_rate_ms;
      data.enabled = gw.enabled;
      data.connection_config = gw.connection_config;
      break;
    case 'tag':
      const tag = node.data as any;
      data.code = tag.code;
      data.alias = tag.alias;
      data.data_type = tag.data_type;
      data.historize = tag.historize;
      data.historize_deadband = tag.historize_deadband;
      data.alarm_enabled = tag.alarm_enabled;
      data.alarm_threshold = tag.alarm_threshold;
      data.alarm_operator = tag.alarm_operator;
      data.alarm_priority = tag.alarm_priority;
      break;
  }

  return data;
}

function getDisplayFields(node: TreeNode): [string, any][] {
  const fields: [string, any][] = [];

  switch (node.type) {
    case 'site':
      fields.push(['Name', node.name]);
      fields.push(['Created At', new Date((node.data as any).created_at).toLocaleString()]);
      break;
    case 'area':
      fields.push(['Name', node.name]);
      fields.push(['Created At', new Date((node.data as any).created_at).toLocaleString()]);
      break;
    case 'gateway':
      const gw = node.data as any;
      fields.push(['Name', gw.name]);
      fields.push(['Driver Type', gw.driver_type]);
      fields.push(['Scan Rate', `${gw.scan_rate_ms}ms`]);
      fields.push(['Enabled', gw.enabled ? 'Yes' : 'No']);
      if (node.connectionStatus) {
        fields.push(['Connection Status', node.connectionStatus]);
      }
      if (node.lastSeen) {
        fields.push(['Last Seen', new Date(node.lastSeen).toLocaleString()]);
      }
      fields.push(['Connection Config', JSON.stringify(gw.connection_config, null, 2)]);
      fields.push(['Created At', new Date(gw.created_at).toLocaleString()]);
      break;
    case 'tag':
      const tag = node.data as any;
      fields.push(['Code', tag.code]);
      fields.push(['Alias', tag.alias]);
      fields.push(['Data Type', tag.data_type]);
      fields.push(['Historize', tag.historize ? 'Yes' : 'No']);
      fields.push(['Historize Deadband', tag.historize_deadband]);
      fields.push(['Alarm Enabled', tag.alarm_enabled ? 'Yes' : 'No']);
      if (tag.alarm_enabled) {
        fields.push(['Alarm Threshold', tag.alarm_threshold]);
        fields.push(['Alarm Operator', tag.alarm_operator || 'N/A']);
        fields.push(['Alarm Priority', tag.alarm_priority]);
      }
      fields.push(['Created At', new Date(tag.created_at).toLocaleString()]);
      break;
  }

  return fields;
}

function getEditableFields(node: TreeNode): [string, string, string, { label: string; value: string }[] | undefined][] {
  const fields: [string, string, string, { label: string; value: string }[] | undefined][] = [];

  switch (node.type) {
    case 'gateway':
      fields.push(['Name', 'name', 'text', undefined]);
      fields.push(['Scan Rate (ms)', 'scan_rate_ms', 'number', undefined]);
      fields.push(['Enabled', 'enabled', 'checkbox', undefined]);
      fields.push(['Connection Config', 'connection_config', 'json', undefined]);
      break;
    case 'tag':
      fields.push(['Code', 'code', 'text', undefined]);
      fields.push(['Alias', 'alias', 'text', undefined]);
      fields.push(['Data Type', 'data_type', 'select', [
        { label: 'INT', value: 'INT' },
        { label: 'REAL', value: 'REAL' },
        { label: 'BOOL', value: 'BOOL' },
        { label: 'DINT', value: 'DINT' },
      ]]);
      fields.push(['Historize', 'historize', 'checkbox', undefined]);
      fields.push(['Historize Deadband', 'historize_deadband', 'number', undefined]);
      fields.push(['Alarm Enabled', 'alarm_enabled', 'checkbox', undefined]);
      fields.push(['Alarm Threshold', 'alarm_threshold', 'number', undefined]);
      fields.push(['Alarm Operator', 'alarm_operator', 'select', [
        { label: 'Greater Than (>)', value: '>' },
        { label: 'Less Than (<)', value: '<' },
        { label: 'Equal (=)', value: '=' },
      ]]);
      fields.push(['Alarm Priority', 'alarm_priority', 'number', undefined]);
      break;
  }

  return fields;
}

function formatValue(value: any): string {
  if (value === null || value === undefined) {
    return 'N/A';
  }
  if (typeof value === 'boolean') {
    return value ? 'Yes' : 'No';
  }
  if (typeof value === 'object') {
    return JSON.stringify(value, null, 2);
  }
  return String(value);
}
