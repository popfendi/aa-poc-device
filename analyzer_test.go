package main

import "testing"

func Test_convertToCalibratedDB(t *testing.T) {
	type args struct {
		dBFSValue         float64
		calibrationOffset float64
	}
	tests := []struct {
		name string
		args args
		want float64
	}{
		{name: "1", args: args{dBFSValue: -180.00, calibrationOffset: 160}, want: 20.00},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertToCalibratedDB(tt.args.dBFSValue, tt.args.calibrationOffset); got != tt.want {
				t.Errorf("convertToCalibratedDB() = %v, want %v", got, tt.want)
			} else {
				t.Logf("convertToCalibratedDB() = %v, want %v", got, tt.want)
			}
		})
	}
}
