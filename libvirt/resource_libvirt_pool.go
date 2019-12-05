package libvirt

import (
	"encoding/xml"
	"fmt"
	"log"

	"github.com/hashicorp/terraform/helper/schema"
	libvirt "github.com/libvirt/libvirt-go"
	"github.com/libvirt/libvirt-go-xml"
)

func resourceLibvirtPool() *schema.Resource {
	return &schema.Resource{
		Create: resourceLibvirtPoolCreate,
		Read:   resourceLibvirtPoolRead,
		Delete: resourceLibvirtPoolDelete,
		Exists: resourceLibvirtPoolExists,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"type": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"capacity": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"allocation": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"available": {
				Type:     schema.TypeString,
				Computed: true,
				Optional: true,
				ForceNew: true,
			},
			"xml": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"xslt": {
							Type:     schema.TypeString,
							Optional: true,
							ForceNew: true,
						},
					},
				},
			},
			"source_devices": {
				Type: schema.TypeList,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional: true,
				ForceNew: true,
			},

			"path": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},
		},
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
	}
}

func buildLibvirtStoragePoolDef(d *schema.ResourceData) (*libvirtxml.StoragePool, error) {
	poolName := d.Get("name").(string)
	poolType := d.Get("type").(string)
	switch poolType {
	case "dir":
		poolPath := d.Get("path").(string)
		if poolPath == "" {
			return nil, fmt.Errorf(`"path" attribute is required for storage pools of type "dir"`)
		}
		return &libvirtxml.StoragePool{
			Type: "dir",
			Name: poolName,
			Target: &libvirtxml.StoragePoolTarget{
				Path: poolPath,
			},
		}, nil
	case "logical":
		sourceDevicesPaths := d.Get("source_devices").([]interface{})
		if len(sourceDevicesPaths) == 0 {
			return nil, fmt.Errorf(`Non-empty "source_devices" attribute is required for storage pools of type "logical"`)
		}
		sourceDevices := make([]libvirtxml.StoragePoolSourceDevice, 0, len(sourceDevicesPaths))
		for _, path := range sourceDevicesPaths {
			sourceDevices = append(sourceDevices, libvirtxml.StoragePoolSourceDevice{Path: path.(string)})
		}
		return &libvirtxml.StoragePool{
			Type: "logical",
			Name: poolName,
			Source: &libvirtxml.StoragePoolSource{
				Device: sourceDevices,
			},
		}, nil
	case "fs":
		fallthrough
	case "netfs":
		fallthrough
	case "disk":
		fallthrough
	case "scsi":
		fallthrough
	case "iscsi":
		fallthrough
	case "iscsi-direct":
		fallthrough
	case "mpath":
		fallthrough
	case "rbd":
		fallthrough
	case "sheepdog":
		fallthrough
	case "gluster":
		fallthrough
	case "zfs":
		fallthrough
	case "vstorage":
		return nil, fmt.Errorf("Libvirt storage pools of type '%s' are not supported yet", poolType)
	default:
		return nil, fmt.Errorf("Unrecognized pool type '%s'", poolType)
	}
}

func resourceLibvirtPoolCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)
	if client.libvirt == nil {
		return fmt.Errorf(LibVirtConIsNil)
	}

	poolName := d.Get("name").(string)

	client.poolMutexKV.Lock(poolName)
	defer client.poolMutexKV.Unlock(poolName)

	// Check whether the storage pool already exists. Its name needs to be
	// unique.
	if _, err := client.libvirt.LookupStoragePoolByName(poolName); err == nil {
		return fmt.Errorf("storage pool '%s' already exists", poolName)
	}
	log.Printf("[DEBUG] Pool with name '%s' does not exist yet", poolName)

	poolDef, err := buildLibvirtStoragePoolDef(d)
	if err != nil {
		return fmt.Errorf("Failed to build storage pool definition: %s", err)
	}

	data, err := xmlMarshallIndented(poolDef)
	if err != nil {
		return fmt.Errorf("Error serializing libvirt storage pool: %s", err)
	}
	log.Printf("[DEBUG] Generated XML for libvirt storage pool:\n%s", data)

	data, err = transformResourceXML(data, d)
	if err != nil {
		return fmt.Errorf("Error applying XSLT stylesheet: %s", err)
	}

	// create the pool
	pool, err := client.libvirt.StoragePoolDefineXML(data, 0)
	if err != nil {
		return fmt.Errorf("Error creating libvirt storage pool: %s", err)
	}
	defer pool.Free()

	err = pool.Build(0)
	if err != nil {
		return fmt.Errorf("Error building libvirt storage pool: %s", err)
	}

	err = pool.SetAutostart(true)
	if err != nil {
		return fmt.Errorf("Error setting up libvirt storage pool: %s", err)
	}

	err = pool.Create(0)
	if err != nil {
		return fmt.Errorf("Error starting libvirt storage pool: %s", err)
	}

	err = pool.Refresh(0)
	if err != nil {
		return fmt.Errorf("Error refreshing libvirt storage pool: %s", err)
	}

	id, err := pool.GetUUIDString()
	if err != nil {
		return fmt.Errorf("Error retrieving libvirt pool id: %s", err)
	}
	d.SetId(id)

	// make sure we record the id even if the rest of this gets interrupted
	d.Partial(true)
	d.Set("id", id)
	d.SetPartial("id")
	d.Partial(false)

	log.Printf("[INFO] Pool ID: %s", d.Id())

	if err := poolWaitForExists(client.libvirt, id); err != nil {
		return err
	}

	return resourceLibvirtPoolRead(d, meta)
}

func resourceLibvirtPoolRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)
	virConn := client.libvirt
	if virConn == nil {
		return fmt.Errorf(LibVirtConIsNil)
	}

	pool, err := virConn.LookupStoragePoolByUUIDString(d.Id())
	if pool == nil {
		log.Printf("storage pool '%s' may have been deleted outside Terraform", d.Id())
		d.SetId("")
		return nil
	}
	defer pool.Free()

	poolName, err := pool.GetName()
	if err != nil {
		return fmt.Errorf("error retrieving pool name: %s", err)
	}
	d.Set("name", poolName)

	info, err := pool.GetInfo()
	if err != nil {
		return fmt.Errorf("error retrieving pool info: %s", err)
	}
	d.Set("capacity", info.Capacity)
	d.Set("allocation", info.Allocation)
	d.Set("available", info.Available)

	poolDefXML, err := pool.GetXMLDesc(0)
	if err != nil {
		return fmt.Errorf("could not get XML description for pool %s: %s", poolName, err)
	}

	var poolDef libvirtxml.StoragePool
	err = xml.Unmarshal([]byte(poolDefXML), &poolDef)
	if err != nil {
		return fmt.Errorf("could not get a pool definition from XML for %s: %s", poolDef.Name, err)
	}

	var poolPath string
	if poolDef.Target != nil && poolDef.Target.Path != "" {
		poolPath = poolDef.Target.Path
	}

	if poolPath == "" {
		log.Printf("Pool %s has no path specified", poolName)
	} else {
		log.Printf("[DEBUG] Pool %s path: %s", poolName, poolPath)
		d.Set("path", poolPath)
	}

	if poolDef.Source != nil && poolDef.Source.Device != nil {
		sourceDevicesPaths := make([]string, 0, len(poolDef.Source.Device))
		for _, device := range poolDef.Source.Device {
			sourceDevicesPaths = append(sourceDevicesPaths, device.Path)
		}
		log.Printf("[DEBUG] Pool %s source devices: %v", poolName, sourceDevicesPaths)
		d.Set("source_devices", sourceDevicesPaths)
	}

	return nil
}

func resourceLibvirtPoolDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)
	if client.libvirt == nil {
		return fmt.Errorf(LibVirtConIsNil)
	}

	return deletePool(client, d.Id())
}

func resourceLibvirtPoolExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	log.Printf("[DEBUG] Check if resource libvirt_pool exists")
	client := meta.(*Client)
	virConn := client.libvirt
	if virConn == nil {
		return false, fmt.Errorf(LibVirtConIsNil)
	}

	pool, err := virConn.LookupStoragePoolByUUIDString(d.Id())
	if err != nil {
		virErr := err.(libvirt.Error)
		if virErr.Code != libvirt.ERR_NO_STORAGE_POOL {
			return false, fmt.Errorf("Can't retrieve pool %s", d.Id())
		}
		// does not exist, but no error
		return false, nil
	}
	defer pool.Free()

	return true, nil
}
