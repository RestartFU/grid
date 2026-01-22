package specs

import "runtime"

type Specs struct {
	Model       string
	Cores       int
	Threads     int
	Motherboard string
	CPUTemp     string
	RAM         string
	RAMSpeed    string
}

func Model() (string, error) {
	specs, err := ReadSpecs()
	if err != nil {
		return "", err
	}
	return specs.Model, nil
}

func ReadSpecs() (Specs, error) {
	model, cores, err := readCPUInfo()
	if err != nil {
		return Specs{}, err
	}

	return Specs{
		Model:       model,
		Cores:       cores,
		Threads:     runtime.NumCPU(),
		Motherboard: readMotherboard(),
		CPUTemp:     readCPUTemp(),
		RAM:         readRAM(),
		RAMSpeed:    readRAMSpeed(),
	}, nil
}
